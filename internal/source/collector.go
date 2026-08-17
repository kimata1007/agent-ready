package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kimata1007/agent-ready/internal/project"
)

const maxDocumentBytes = 20 << 20

var sensitiveQueryKey = regexp.MustCompile(`(?i)(token|key|secret|signature|password|credential|auth)`)

type Runner interface {
	Output(context.Context, string, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, directory, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type Input struct {
	Value   string
	Name    string
	Content []byte
}

type Collected struct {
	Source    project.Source
	Workspace string
	Cleanup   func() error
}

type Collector struct {
	Store      project.Store
	HTTPClient *http.Client
	Runner     Runner
	Now        func() time.Time
}

func (collector Collector) Collect(
	ctx context.Context,
	projectName string,
	input Input,
) (Collected, error) {
	if strings.TrimSpace(input.Value) == "" {
		return Collected{}, errors.New("source is empty")
	}
	if input.Value == "-" {
		return collector.collectText(projectName, input)
	}
	if remote, kind, ok, err := classifyRemote(input.Value); err != nil {
		return Collected{}, err
	} else if ok {
		if kind == "git" {
			return collector.collectRemoteGit(ctx, projectName, remote, input.Name)
		}
		return collector.collectWeb(ctx, projectName, remote, input.Name)
	}
	absolute, err := filepath.Abs(input.Value)
	if err != nil {
		return Collected{}, fmt.Errorf("resolve source path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Collected{}, fmt.Errorf("inspect source %q: %w", input.Value, err)
	}
	if info.IsDir() {
		return collector.collectDirectory(ctx, absolute, input.Name)
	}
	if !info.Mode().IsRegular() {
		return Collected{}, fmt.Errorf("source %q is not a regular file or directory", input.Value)
	}
	return collector.collectFile(projectName, absolute, input.Name)
}

func (collector Collector) Refresh(
	ctx context.Context,
	projectName string,
	existing project.Source,
) (Collected, error) {
	var (
		collected Collected
		err       error
	)
	switch existing.Kind {
	case "text":
		content, readErr := collector.Store.ReadObject(projectName, existing.Object)
		if readErr != nil {
			return Collected{}, readErr
		}
		collected, err = collector.collectText(projectName, Input{
			Value:   "-",
			Name:    existing.Name,
			Content: content,
		})
	case "web":
		collected, err = collector.collectWeb(ctx, projectName, existing.Locator, existing.Name)
	case "git":
		if isRemote(existing.Locator) {
			collected, err = collector.collectRemoteGit(ctx, projectName, existing.Locator, existing.Name)
		} else {
			collected, err = collector.collectDirectory(ctx, existing.Locator, existing.Name)
		}
	case "directory":
		collected, err = collector.collectDirectory(ctx, existing.Locator, existing.Name)
	case "file":
		collected, err = collector.collectFile(projectName, existing.Locator, existing.Name)
	default:
		return Collected{}, fmt.Errorf("unsupported stored source kind %q", existing.Kind)
	}
	if err != nil {
		return Collected{}, err
	}
	collected.Source.ID = existing.ID
	collected.Source.AddedAt = existing.AddedAt
	collected.Source.AnalysisFile = existing.AnalysisFile
	return collected, nil
}

func (collector Collector) collectText(projectName string, input Input) (Collected, error) {
	if len(input.Content) == 0 {
		return Collected{}, errors.New("standard input is empty")
	}
	if len(input.Content) > maxDocumentBytes {
		return Collected{}, errors.New("standard input exceeds 20 MiB")
	}
	digest, err := collector.Store.PutObject(projectName, input.Content)
	if err != nil {
		return Collected{}, err
	}
	workspace, cleanup, err := documentWorkspace("input.md", input.Content)
	if err != nil {
		return Collected{}, err
	}
	name := sourceName(input.Name, "pasted-text")
	return Collected{
		Source:    collector.record(name, "text", "stdin", "", digest, digest),
		Workspace: workspace,
		Cleanup:   cleanup,
	}, nil
}

func (collector Collector) collectFile(projectName, absolute, requestedName string) (Collected, error) {
	content, err := readLimitedFile(absolute)
	if err != nil {
		return Collected{}, err
	}
	digest, err := collector.Store.PutObject(projectName, content)
	if err != nil {
		return Collected{}, err
	}
	workspace, cleanup, err := documentWorkspace(filepath.Base(absolute), content)
	if err != nil {
		return Collected{}, err
	}
	name := sourceName(requestedName, strings.TrimSuffix(filepath.Base(absolute), filepath.Ext(absolute)))
	return Collected{
		Source:    collector.record(name, "file", absolute, "", digest, digest),
		Workspace: workspace,
		Cleanup:   cleanup,
	}, nil
}

func (collector Collector) collectWeb(
	ctx context.Context,
	projectName, locator, requestedName string,
) (Collected, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, locator, nil)
	if err != nil {
		return Collected{}, fmt.Errorf("create web request: %w", err)
	}
	request.Header.Set("User-Agent", "agent-ready/1")
	client := collector.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return Collected{}, fmt.Errorf("fetch %s: %w", locator, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Collected{}, fmt.Errorf("fetch %s: HTTP %s", locator, response.Status)
	}
	content, err := readLimited(response.Body)
	if err != nil {
		return Collected{}, fmt.Errorf("read %s: %w", locator, err)
	}
	digest, err := collector.Store.PutObject(projectName, content)
	if err != nil {
		return Collected{}, err
	}
	parsed, _ := url.Parse(locator)
	base := strings.TrimSuffix(filepath.Base(parsed.Path), filepath.Ext(parsed.Path))
	if base == "" || base == "." || base == "/" {
		base = parsed.Hostname()
	}
	name := sourceName(requestedName, base)
	workspace, cleanup, err := documentWorkspace("document.md", content)
	if err != nil {
		return Collected{}, err
	}
	return Collected{
		Source:    collector.record(name, "web", locator, "", digest, digest),
		Workspace: workspace,
		Cleanup:   cleanup,
	}, nil
}

func (collector Collector) collectDirectory(
	ctx context.Context,
	absolute, requestedName string,
) (Collected, error) {
	digest, err := digestDirectory(absolute)
	if err != nil {
		return Collected{}, err
	}
	kind := "directory"
	revision := ""
	if _, err := os.Stat(filepath.Join(absolute, ".git")); err == nil {
		kind = "git"
		revision = collector.gitRevision(ctx, absolute)
	}
	name := sourceName(requestedName, filepath.Base(absolute))
	return Collected{
		Source:    collector.record(name, kind, absolute, revision, digest, ""),
		Workspace: absolute,
		Cleanup:   func() error { return nil },
	}, nil
}

func (collector Collector) collectRemoteGit(
	ctx context.Context,
	projectName, locator, requestedName string,
) (Collected, error) {
	temporary, err := os.MkdirTemp("", "agent-ready-git-*")
	if err != nil {
		return Collected{}, fmt.Errorf("create Git workspace: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(temporary) }
	runner := collector.runner()
	if _, err := runner.Output(
		ctx,
		"",
		"git",
		"clone",
		"--quiet",
		"--depth",
		"1",
		"--no-tags",
		locator,
		temporary,
	); err != nil {
		_ = cleanup()
		return Collected{}, fmt.Errorf("clone %s: %w", locator, err)
	}
	digest, err := digestDirectory(temporary)
	if err != nil {
		_ = cleanup()
		return Collected{}, err
	}
	revision := collector.gitRevision(ctx, temporary)
	parsedName := strings.TrimSuffix(filepath.Base(strings.TrimSuffix(locator, "/")), ".git")
	name := sourceName(requestedName, parsedName)
	return Collected{
		Source:    collector.record(name, "git", locator, revision, digest, ""),
		Workspace: temporary,
		Cleanup:   cleanup,
	}, nil
}

func (collector Collector) runner() Runner {
	if collector.Runner != nil {
		return collector.Runner
	}
	return ExecRunner{}
}

func (collector Collector) gitRevision(ctx context.Context, directory string) string {
	output, err := collector.runner().Output(ctx, directory, "git", "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (collector Collector) record(name, kind, locator, revision, digest, object string) project.Source {
	now := time.Now().UTC()
	if collector.Now != nil {
		now = collector.Now().UTC()
	}
	identity := locator
	if kind == "text" {
		identity = digest
	}
	return project.Source{
		ID:        sourceID(name, kind+"\x00"+identity),
		Name:      name,
		Kind:      kind,
		Locator:   locator,
		Revision:  revision,
		Digest:    digest,
		Object:    object,
		AddedAt:   now,
		CheckedAt: now,
	}
}

func classifyRemote(value string) (string, string, bool, error) {
	if strings.HasPrefix(value, "git+") {
		value = strings.TrimPrefix(value, "git+")
		if err := validateRemote(value); err != nil {
			return "", "", false, err
		}
		return value, "git", true, nil
	}
	if strings.HasPrefix(value, "git@") {
		return value, "git", true, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return "", "", false, nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "ssh" {
		return "", "", false, fmt.Errorf("unsupported source URL scheme %q", parsed.Scheme)
	}
	if err := validateRemote(value); err != nil {
		return "", "", false, err
	}
	kind := "web"
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "ssh" || strings.HasSuffix(parsed.Path, ".git") ||
		host == "github.com" || host == "gitlab.com" || host == "bitbucket.org" {
		kind = "git"
	}
	return value, kind, true, nil
}

func validateRemote(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse source URL: %w", err)
	}
	if parsed.User != nil {
		return errors.New("source URLs must not contain user information or credentials")
	}
	for key := range parsed.Query() {
		if sensitiveQueryKey.MatchString(key) {
			return fmt.Errorf("source URL contains sensitive query parameter %q", key)
		}
	}
	return nil
}

func isRemote(locator string) bool {
	if strings.HasPrefix(locator, "git@") {
		return true
	}
	parsed, err := url.Parse(locator)
	return err == nil && parsed.Scheme != ""
}

func sourceName(requested, fallback string) string {
	if cleaned := strings.TrimSpace(requested); cleaned != "" {
		return cleaned
	}
	if cleaned := strings.TrimSpace(fallback); cleaned != "" {
		return cleaned
	}
	return "source"
}

func sourceID(name, identity string) string {
	slug := strings.Trim(slugExpression.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "source"
	}
	sum := sha256.Sum256([]byte(identity))
	return slug + "-" + hex.EncodeToString(sum[:5])
}

var slugExpression = regexp.MustCompile(`[^a-z0-9]+`)

func documentWorkspace(filename string, content []byte) (string, func() error, error) {
	directory, err := os.MkdirTemp("", "agent-ready-document-*")
	if err != nil {
		return "", nil, fmt.Errorf("create document workspace: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(directory) }
	filename = filepath.Base(filename)
	if filename == "." || filename == "" {
		filename = "document.txt"
	}
	if err := os.WriteFile(filepath.Join(directory, filename), content, 0o600); err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("prepare document workspace: %w", err)
	}
	return directory, cleanup, nil
}

func readLimitedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	content, err := readLimited(file)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return content, nil
}

func readLimited(reader io.Reader) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxDocumentBytes {
		return nil, errors.New("document exceeds 20 MiB")
	}
	return content, nil
}

func digestDirectory(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("inventory %s: %w", root, err)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, relative := range paths {
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("inspect %s: %w", path, err)
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(relative))
		_, _ = hash.Write([]byte{0})
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", fmt.Errorf("read symlink %s: %w", path, err)
			}
			_, _ = io.WriteString(hash, target)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", path, err)
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", fmt.Errorf("hash %s: %w", path, copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close %s: %w", path, closeErr)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ignoredDirectory(name string) bool {
	switch name {
	case ".git", ".agent-ready", "node_modules", "vendor", ".venv", "dist", "build", "target":
		return true
	default:
		return false
	}
}
