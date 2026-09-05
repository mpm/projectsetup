package generate

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/mpm/projectsetup/internal/config"
)

//go:embed templates/*
var templateFiles embed.FS

type data struct {
	Config           config.Config
	AITools          string
	Codex            bool
	CodexStateSource string
}

func templateData(cfg config.Config) data {
	result := data{Config: cfg, AITools: shellWords(cfg.AITools), CodexStateSource: config.CodexStateSource}
	for _, tool := range cfg.AITools {
		if tool == config.AIToolCodex {
			result.Codex = true
		}
	}
	return result
}

func executeTemplate(name string, value data) ([]byte, error) {
	tmpl, err := template.New(name).ParseFS(templateFiles, "templates/"+name)
	if err != nil {
		return nil, fmt.Errorf("parse embedded template %s: %w", name, err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, value); err != nil {
		return nil, fmt.Errorf("render embedded template %s: %w", name, err)
	}
	return output.Bytes(), nil
}
