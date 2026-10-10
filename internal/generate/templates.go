package generate

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
)

//go:embed templates/*
var templateFiles embed.FS

type data struct {
	Config   config.Config
	Resolved presets.Resolved
	AITools  string
	// AptGroups holds remaining core packages, contributing definition blocks,
	// and explicit system packages in installation order, one line per group.
	AptGroups []string
	Setup     string
}

func templateData(cfg config.Config, resolved presets.Resolved) data {
	result := data{
		Config:   cfg,
		Resolved: resolved,
		AITools:  shellWords(cfg.AITools),
		Setup:    strings.Join(resolved.Setup, "\n"),
	}
	if core := resolved.CoreAptPackages(); len(core) > 0 {
		result.AptGroups = append(result.AptGroups, strings.Join(core, " "))
	}
	for _, group := range resolved.Apt {
		result.AptGroups = append(result.AptGroups, strings.Join(group, " "))
	}
	result.AptGroups = append(result.AptGroups, cfg.SystemPackages...)
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
