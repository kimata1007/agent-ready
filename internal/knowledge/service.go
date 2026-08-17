package knowledge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kimata1007/agent-ready/internal/analyzer"
	"github.com/kimata1007/agent-ready/internal/project"
	"github.com/kimata1007/agent-ready/internal/source"
)

type AnalyzerFactory func(project.AnalyzerConfig) (analyzer.Analyzer, error)

type AgentIntegration interface {
	Ensure() error
}

type Service struct {
	Store       project.Store
	Collector   source.Collector
	NewAnalyzer AnalyzerFactory
	Integration AgentIntegration
	Now         func() time.Time
}

type InitOptions struct {
	Project  string
	Analyzer project.AnalyzerConfig
	Sources  []source.Input
}

type AddOptions struct {
	Project string
	Sources []source.Input
}

type Result struct {
	Project        string   `json:"project"`
	Added          []string `json:"added,omitempty"`
	Changed        []string `json:"changed,omitempty"`
	UnchangedCount int      `json:"unchangedCount,omitempty"`
	SourceCount    int      `json:"sourceCount"`
	CatalogUpdated bool     `json:"catalogUpdated"`
}

func (service Service) Init(ctx context.Context, options InitOptions) (result Result, resultErr error) {
	if len(options.Sources) == 0 {
		return Result{}, errors.New("init requires at least one source")
	}
	if options.Analyzer.Provider == "" {
		options.Analyzer.Provider = "codex"
	}
	now := service.now()
	projectValue := project.Project{
		SchemaVersion: project.SchemaVersion,
		Name:          options.Project,
		Analyzer:      options.Analyzer,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := service.analyzer(projectValue.Analyzer); err != nil {
		return Result{}, err
	}
	if err := service.Store.Create(projectValue); err != nil {
		return Result{}, err
	}
	complete := false
	defer func() {
		if !complete {
			removeErr := service.Store.Remove(options.Project)
			_ = service.Store.RebuildIndex(service.now())
			if removeErr != nil && resultErr == nil {
				resultErr = removeErr
			}
		}
	}()
	result, resultErr = service.add(ctx, projectValue, options.Sources)
	if resultErr != nil {
		return Result{}, resultErr
	}
	if resultErr = service.afterChange(); resultErr != nil {
		return Result{}, resultErr
	}
	complete = true
	return result, nil
}

func (service Service) Add(ctx context.Context, options AddOptions) (Result, error) {
	if len(options.Sources) == 0 {
		return Result{}, errors.New("add requires at least one source")
	}
	projectValue, err := service.Store.LoadProject(options.Project)
	if err != nil {
		return Result{}, err
	}
	result, err := service.add(ctx, projectValue, options.Sources)
	if err != nil {
		return Result{}, err
	}
	if err := service.afterChange(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (service Service) Refresh(ctx context.Context, projectName string) (Result, error) {
	projectValue, err := service.Store.LoadProject(projectName)
	if err != nil {
		return Result{}, err
	}
	sourcesValue, err := service.Store.LoadSources(projectName)
	if err != nil {
		return Result{}, err
	}
	if len(sourcesValue.Sources) == 0 {
		return Result{}, errors.New("project has no registered sources")
	}
	client, err := service.analyzer(projectValue.Analyzer)
	if err != nil {
		return Result{}, err
	}
	updated := append([]project.Source(nil), sourcesValue.Sources...)
	changed := make([]string, 0)
	unchanged := 0
	for index, existing := range sourcesValue.Sources {
		collected, err := service.collector().Refresh(ctx, projectName, existing)
		if err != nil {
			return Result{}, fmt.Errorf("refresh source %q: %w", existing.Name, err)
		}
		if collected.Cleanup == nil {
			collected.Cleanup = func() error { return nil }
		}
		if collected.Source.Digest == existing.Digest && collected.Source.Revision == existing.Revision {
			updated[index].CheckedAt = collected.Source.CheckedAt
			unchanged++
			_ = collected.Cleanup()
			continue
		}
		analysis, analyzeErr := client.Analyze(ctx, collected.Source, collected.Workspace)
		cleanupErr := collected.Cleanup()
		if analyzeErr != nil {
			return Result{}, fmt.Errorf("analyze source %q: %w", existing.Name, analyzeErr)
		}
		if cleanupErr != nil {
			return Result{}, fmt.Errorf("clean source %q workspace: %w", existing.Name, cleanupErr)
		}
		analysisFile, err := service.Store.SaveAnalysis(projectName, analysis)
		if err != nil {
			return Result{}, err
		}
		collected.Source.AnalysisFile = analysisFile
		updated[index] = collected.Source
		changed = append(changed, collected.Source.ID)
	}
	sourcesValue.Sources = updated
	sourcesValue.UpdatedAt = service.now()
	if len(changed) == 0 {
		if err := service.Store.SaveSources(sourcesValue); err != nil {
			return Result{}, err
		}
		if err := service.afterChange(); err != nil {
			return Result{}, err
		}
		return Result{
			Project:        projectName,
			UnchangedCount: unchanged,
			SourceCount:    len(updated),
			CatalogUpdated: false,
		}, nil
	}
	if err := service.regenerate(ctx, client, &projectValue, sourcesValue); err != nil {
		return Result{}, err
	}
	if err := service.afterChange(); err != nil {
		return Result{}, err
	}
	return Result{
		Project:        projectName,
		Changed:        changed,
		UnchangedCount: unchanged,
		SourceCount:    len(updated),
		CatalogUpdated: true,
	}, nil
}

func (service Service) add(
	ctx context.Context,
	projectValue project.Project,
	inputs []source.Input,
) (Result, error) {
	sourcesValue, err := service.Store.LoadSources(projectValue.Name)
	if err != nil {
		return Result{}, err
	}
	client, err := service.analyzer(projectValue.Analyzer)
	if err != nil {
		return Result{}, err
	}
	existing := make(map[string]struct{}, len(sourcesValue.Sources))
	for _, saved := range sourcesValue.Sources {
		existing[sourceIdentity(saved)] = struct{}{}
	}
	added := make([]string, 0, len(inputs))
	updated := append([]project.Source(nil), sourcesValue.Sources...)
	for _, input := range inputs {
		collected, err := service.collector().Collect(ctx, projectValue.Name, input)
		if err != nil {
			return Result{}, err
		}
		if collected.Cleanup == nil {
			collected.Cleanup = func() error { return nil }
		}
		identity := sourceIdentity(collected.Source)
		if _, duplicate := existing[identity]; duplicate {
			_ = collected.Cleanup()
			return Result{}, fmt.Errorf("source %q is already registered", collected.Source.Name)
		}
		analysis, analyzeErr := client.Analyze(ctx, collected.Source, collected.Workspace)
		cleanupErr := collected.Cleanup()
		if analyzeErr != nil {
			return Result{}, fmt.Errorf("analyze source %q: %w", collected.Source.Name, analyzeErr)
		}
		if cleanupErr != nil {
			return Result{}, fmt.Errorf("clean source %q workspace: %w", collected.Source.Name, cleanupErr)
		}
		analysisFile, err := service.Store.SaveAnalysis(projectValue.Name, analysis)
		if err != nil {
			return Result{}, err
		}
		collected.Source.AnalysisFile = analysisFile
		existing[identity] = struct{}{}
		updated = append(updated, collected.Source)
		added = append(added, collected.Source.ID)
	}
	sourcesValue.Sources = updated
	sourcesValue.UpdatedAt = service.now()
	if err := service.regenerate(ctx, client, &projectValue, sourcesValue); err != nil {
		return Result{}, err
	}
	return Result{
		Project:        projectValue.Name,
		Added:          added,
		SourceCount:    len(updated),
		CatalogUpdated: true,
	}, nil
}

func (service Service) regenerate(
	ctx context.Context,
	client analyzer.Analyzer,
	projectValue *project.Project,
	sourcesValue project.Sources,
) error {
	analyses := make([]project.SourceAnalysis, 0, len(sourcesValue.Sources))
	for _, saved := range sourcesValue.Sources {
		analysis, err := service.Store.LoadAnalysis(projectValue.Name, saved.AnalysisFile)
		if err != nil {
			return err
		}
		analyses = append(analyses, analysis)
	}
	synthesis, err := client.Synthesize(ctx, *projectValue, analyses)
	if err != nil {
		return fmt.Errorf("synthesize project catalog: %w", err)
	}
	projectValue.UpdatedAt = service.now()
	catalog, err := buildCatalog(*projectValue, sourcesValue.Sources, analyses, synthesis)
	if err != nil {
		return err
	}
	if err := service.Store.SaveCatalog(catalog, renderCatalog(catalog), renderContext(catalog)); err != nil {
		return err
	}
	if err := service.Store.SaveSources(sourcesValue); err != nil {
		return err
	}
	return service.Store.SaveProject(*projectValue)
}

func (service Service) analyzer(settings project.AnalyzerConfig) (analyzer.Analyzer, error) {
	if service.NewAnalyzer != nil {
		return service.NewAnalyzer(settings)
	}
	return analyzer.New(settings, nil)
}

func (service Service) collector() source.Collector {
	collector := service.Collector
	collector.Store = service.Store
	if collector.Now == nil {
		collector.Now = service.now
	}
	return collector
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

func (service Service) afterChange() error {
	if err := service.Store.RebuildIndex(service.now()); err != nil {
		return err
	}
	if service.Integration != nil {
		if err := service.Integration.Ensure(); err != nil {
			return fmt.Errorf("configure agent startup instructions: %w", err)
		}
	}
	return nil
}

func sourceIdentity(saved project.Source) string {
	if saved.Kind == "text" {
		return saved.Kind + "\x00" + saved.Digest
	}
	return saved.Kind + "\x00" + saved.Locator
}
