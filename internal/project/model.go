package project

import "time"

const SchemaVersion = 1

type AnalyzerConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

type Project struct {
	SchemaVersion int            `json:"schemaVersion"`
	Name          string         `json:"name"`
	Analyzer      AnalyzerConfig `json:"analyzer"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type Source struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	Locator      string    `json:"locator"`
	Revision     string    `json:"revision,omitempty"`
	Digest       string    `json:"digest"`
	Object       string    `json:"object,omitempty"`
	AnalysisFile string    `json:"analysisFile"`
	AddedAt      time.Time `json:"addedAt"`
	CheckedAt    time.Time `json:"checkedAt"`
}

type Sources struct {
	SchemaVersion int       `json:"schemaVersion"`
	Project       string    `json:"project"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Sources       []Source  `json:"sources"`
}

type Location struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

type SourceAnalysis struct {
	SchemaVersion int        `json:"schemaVersion"`
	SourceID      string     `json:"sourceId"`
	Summary       string     `json:"summary"`
	Purpose       string     `json:"purpose"`
	KeyConcepts   []string   `json:"keyConcepts"`
	Locations     []Location `json:"locations"`
	Usage         []string   `json:"usage"`
	Warnings      []string   `json:"warnings,omitempty"`
}

type ProjectAnalysis struct {
	Overview       string   `json:"overview"`
	KeyConcepts    []string `json:"keyConcepts"`
	Workflows      []string `json:"workflows"`
	SourceGuidance []string `json:"sourceGuidance"`
}

type CatalogEntry struct {
	SourceID    string     `json:"sourceId"`
	SourceName  string     `json:"sourceName"`
	Kind        string     `json:"kind"`
	Locator     string     `json:"locator"`
	Revision    string     `json:"revision,omitempty"`
	Digest      string     `json:"digest"`
	Summary     string     `json:"summary"`
	Purpose     string     `json:"purpose"`
	KeyConcepts []string   `json:"keyConcepts"`
	Locations   []Location `json:"locations"`
	Usage       []string   `json:"usage"`
	Warnings    []string   `json:"warnings,omitempty"`
}

type Catalog struct {
	SchemaVersion int            `json:"schemaVersion"`
	Project       string         `json:"project"`
	GeneratedAt   time.Time      `json:"generatedAt"`
	Overview      string         `json:"overview"`
	KeyConcepts   []string       `json:"keyConcepts"`
	Workflows     []string       `json:"workflows"`
	SourceGuide   []string       `json:"sourceGuide"`
	Entries       []CatalogEntry `json:"entries"`
}

type IndexSource struct {
	Kind    string `json:"kind"`
	Locator string `json:"locator"`
}

type IndexProject struct {
	Name        string        `json:"name"`
	ContextPath string        `json:"contextPath"`
	CatalogPath string        `json:"catalogPath"`
	Sources     []IndexSource `json:"sources"`
}

type Index struct {
	SchemaVersion int            `json:"schemaVersion"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	Projects      []IndexProject `json:"projects"`
}
