// Package ai wraps the Claude API for Chalked OS's three AI features:
// lesson plans, question papers, and report card remarks. System
// prompts are embedded at build time (never editable from the client,
// never fetched at runtime) and versioned by directory — see the AI
// Prompt Library document for the full text and the reasoning behind
// each instruction.
package ai

import (
	"embed"
	"fmt"
)

//go:embed prompts/lesson_plan/*.txt prompts/question_paper/*.txt prompts/report_card/*.txt
var promptFS embed.FS

// Feature identifies which of the three AI features a prompt is for —
// matches the CHECK constraint on ai_generations.feature.
type Feature string

const (
	FeatureLessonPlan       Feature = "lesson_plan"
	FeatureQuestionPaper    Feature = "question_paper"
	FeatureReportCardRemark Feature = "report_card_remark"
)

// featureDir maps a Feature to its directory under prompts/ — kept
// separate from the Feature value itself since the DB constraint uses
// "report_card_remark" but the directory (and file layout) reads better
// as "report_card".
var featureDir = map[Feature]string{
	FeatureLessonPlan:       "lesson_plan",
	FeatureQuestionPaper:    "question_paper",
	FeatureReportCardRemark: "report_card",
}

// LoadPrompt reads the system prompt text for a feature at a given
// version (e.g. "v1.0"), matching the ai_generations.prompt_version
// column so every generation can be traced back to the exact prompt
// that produced it.
func LoadPrompt(feature Feature, version string) (string, error) {
	dir, ok := featureDir[feature]
	if !ok {
		return "", fmt.Errorf("unknown feature: %s", feature)
	}
	path := fmt.Sprintf("prompts/%s/%s.txt", dir, version)
	content, err := promptFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("loading prompt %s: %w", path, err)
	}
	return string(content), nil
}

// CurrentVersion is the prompt version used for new generations. Bump
// this (and add a new .txt file — never edit an existing version in
// place) when a prompt changes, per the AI Prompt Library document's
// versioning section.
const CurrentVersion = "v1.0"
