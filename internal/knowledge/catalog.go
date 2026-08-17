package knowledge

import (
	"fmt"
	"strings"

	"github.com/kimata1007/agent-ready/internal/project"
)

func buildCatalog(
	projectValue project.Project,
	sources []project.Source,
	analyses []project.SourceAnalysis,
	synthesis project.ProjectAnalysis,
) (project.Catalog, error) {
	bySource := make(map[string]project.SourceAnalysis, len(analyses))
	for _, analysis := range analyses {
		bySource[analysis.SourceID] = analysis
	}
	entries := make([]project.CatalogEntry, 0, len(sources))
	for _, source := range sources {
		analysis, ok := bySource[source.ID]
		if !ok {
			return project.Catalog{}, fmt.Errorf("analysis for source %q is missing", source.ID)
		}
		entries = append(entries, project.CatalogEntry{
			SourceID:    source.ID,
			SourceName:  source.Name,
			Kind:        source.Kind,
			Locator:     source.Locator,
			Revision:    source.Revision,
			Digest:      source.Digest,
			Summary:     analysis.Summary,
			Purpose:     analysis.Purpose,
			KeyConcepts: analysis.KeyConcepts,
			Locations:   analysis.Locations,
			Usage:       analysis.Usage,
			Warnings:    analysis.Warnings,
		})
	}
	return project.Catalog{
		SchemaVersion: project.SchemaVersion,
		Project:       projectValue.Name,
		GeneratedAt:   projectValue.UpdatedAt,
		Overview:      synthesis.Overview,
		KeyConcepts:   synthesis.KeyConcepts,
		Workflows:     synthesis.Workflows,
		SourceGuide:   synthesis.SourceGuidance,
		Entries:       entries,
	}, nil
}

func renderCatalog(catalog project.Catalog) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# %s knowledge catalog\n\n", catalog.Project)
	fmt.Fprintf(&output, "%s\n\n", catalog.Overview)
	renderList(&output, 2, "Key concepts", catalog.KeyConcepts)
	renderList(&output, 2, "Workflows", catalog.Workflows)
	renderList(&output, 2, "Source guide", catalog.SourceGuide)
	output.WriteString("## Registered sources\n\n")
	for _, entry := range catalog.Entries {
		fmt.Fprintf(&output, "### %s\n\n", entry.SourceName)
		fmt.Fprintf(&output, "- Source ID: `%s`\n", entry.SourceID)
		fmt.Fprintf(&output, "- Kind: `%s`\n", entry.Kind)
		fmt.Fprintf(&output, "- Locator: `%s`\n", entry.Locator)
		if entry.Revision != "" {
			fmt.Fprintf(&output, "- Revision: `%s`\n", entry.Revision)
		}
		fmt.Fprintf(&output, "- Content digest: `%s`\n\n", entry.Digest)
		fmt.Fprintf(&output, "%s\n\n", entry.Summary)
		fmt.Fprintf(&output, "Purpose: %s\n\n", entry.Purpose)
		renderList(&output, 4, "Important concepts", entry.KeyConcepts)
		if len(entry.Locations) > 0 {
			output.WriteString("#### Where to look\n\n")
			for _, location := range entry.Locations {
				fmt.Fprintf(&output, "- `%s`: %s\n", location.Path, location.Description)
			}
			output.WriteString("\n")
		}
		renderList(&output, 4, "When to use this source", entry.Usage)
		renderList(&output, 4, "Warnings", entry.Warnings)
	}
	return output.String()
}

func renderContext(catalog project.Catalog) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# Agent-ready context: %s\n\n", catalog.Project)
	output.WriteString("This file is generated. Treat `catalog.json` as the machine-readable index and ")
	output.WriteString("`catalog.md` as the detailed human-readable catalog.\n\n")
	fmt.Fprintf(&output, "%s\n\n", catalog.Overview)
	renderList(&output, 2, "Primary workflows", catalog.Workflows)
	output.WriteString("## Source routing\n\n")
	for _, entry := range catalog.Entries {
		fmt.Fprintf(
			&output,
			"- **%s** (`%s`): %s Consult it for %s\n",
			entry.SourceName,
			entry.SourceID,
			entry.Summary,
			strings.Join(entry.Usage, "; "),
		)
	}
	return output.String()
}

func renderList(output *strings.Builder, level int, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(output, "%s %s\n\n", strings.Repeat("#", level), title)
	for _, value := range values {
		fmt.Fprintf(output, "- %s\n", value)
	}
	output.WriteString("\n")
}
