package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	projectFile = "project.json"
	sourcesFile = "sources.json"
	catalogFile = "catalog.json"
)

var validProjectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type Store struct {
	Root string
}

func DefaultRoot() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("AGENT_READY_HOME")); configured != "" {
		return filepath.Abs(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home: %w", err)
	}
	return filepath.Join(home, ".agent-ready"), nil
}

func ValidateName(name string) error {
	if !validProjectName.MatchString(name) || name == "." || name == ".." {
		return errors.New("project name must be 1-64 letters, numbers, dots, underscores, or hyphens")
	}
	return nil
}

func (store Store) ProjectDir(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(store.Root, name), nil
}

func (store Store) Create(value Project) error {
	directory, err := store.ProjectDir(value.Name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(directory); err == nil {
		return fmt.Errorf("project %q already exists", value.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check project directory: %w", err)
	}
	if err := os.MkdirAll(store.Root, 0o700); err != nil {
		return fmt.Errorf("create agent-ready home: %w", err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}
	now := time.Now().UTC()
	if value.SchemaVersion == 0 {
		value.SchemaVersion = SchemaVersion
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	if err := writeJSON(filepath.Join(directory, projectFile), value); err != nil {
		return err
	}
	return writeJSON(filepath.Join(directory, sourcesFile), Sources{
		SchemaVersion: SchemaVersion,
		Project:       value.Name,
		UpdatedAt:     value.CreatedAt,
		Sources:       []Source{},
	})
}

func (store Store) LoadProject(name string) (Project, error) {
	var value Project
	if err := store.read(name, projectFile, &value); err != nil {
		return Project{}, err
	}
	return value, nil
}

func (store Store) SaveProject(value Project) error {
	directory, err := store.ProjectDir(value.Name)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(directory, projectFile), value)
}

func (store Store) LoadSources(name string) (Sources, error) {
	var value Sources
	if err := store.read(name, sourcesFile, &value); err != nil {
		return Sources{}, err
	}
	if value.Sources == nil {
		value.Sources = []Source{}
	}
	return value, nil
}

func (store Store) SaveSources(value Sources) error {
	directory, err := store.ProjectDir(value.Project)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(directory, sourcesFile), value)
}

func (store Store) SaveAnalysis(projectName string, value SourceAnalysis) (string, error) {
	directory, err := store.ProjectDir(projectName)
	if err != nil {
		return "", err
	}
	if value.SourceID == "" {
		return "", errors.New("analysis source ID is required")
	}
	relative := filepath.Join("analyses", value.SourceID+".json")
	if err := writeJSON(filepath.Join(directory, relative), value); err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

func (store Store) LoadAnalysis(projectName, relative string) (SourceAnalysis, error) {
	var value SourceAnalysis
	directory, err := store.ProjectDir(projectName)
	if err != nil {
		return SourceAnalysis{}, err
	}
	path, err := containedPath(directory, relative)
	if err != nil {
		return SourceAnalysis{}, err
	}
	if err := readJSON(path, &value); err != nil {
		return SourceAnalysis{}, fmt.Errorf("read analysis: %w", err)
	}
	return value, nil
}

func (store Store) SaveCatalog(value Catalog, markdown, context string) error {
	directory, err := store.ProjectDir(value.Project)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(directory, catalogFile), value); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(directory, "catalog.md"), []byte(markdown)); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(directory, "context.md"), []byte(context))
}

func (store Store) PutObject(projectName string, content []byte) (string, error) {
	directory, err := store.ProjectDir(projectName)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	path := filepath.Join(directory, "objects", digest)
	if _, err := os.Stat(path); err == nil {
		return digest, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check object: %w", err)
	}
	if err := atomicWrite(path, content); err != nil {
		return "", err
	}
	return digest, nil
}

func (store Store) ReadObject(projectName, digest string) ([]byte, error) {
	if len(digest) != sha256.Size*2 {
		return nil, errors.New("invalid object digest")
	}
	directory, err := store.ProjectDir(projectName)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filepath.Join(directory, "objects", digest))
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, errors.New("stored object digest does not match content")
	}
	return content, nil
}

func (store Store) read(name, filename string, destination any) error {
	directory, err := store.ProjectDir(name)
	if err != nil {
		return err
	}
	if err := readJSON(filepath.Join(directory, filename), destination); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project %q does not exist", name)
		}
		return err
	}
	return nil
}

func containedPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", errors.New("path must be relative to the project")
	}
	cleaned := filepath.Clean(relative)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the project directory")
	}
	return filepath.Join(root, cleaned), nil
}

func readJSON(path string, destination any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, destination); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	content = append(content, '\n')
	return atomicWrite(path, content)
}

func atomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agent-ready-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set permissions for %s: %w", path, err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
