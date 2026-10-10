package presets

import (
	"reflect"
	"strings"
	"testing"
)

const minimalPreset = `
schema = 1
kind = "preset"
name = "base"
version = "1.0.0"
description = "Test preset"

[image]
base = "debian:trixie"
`

func TestParseAcceptsMinimalDefinitions(t *testing.T) {
	definition, err := Parse([]byte(minimalPreset), "base.toml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if definition.Name != "base" || definition.Kind != KindPreset || definition.Image.Base != "debian:trixie" {
		t.Fatalf("Parse() = %+v", definition)
	}
	addon := "schema = 1\nkind = \"addon\"\nname = \"tool\"\nversion = \"0.1.0\"\ndescription = \"Test add-on\"\n"
	if _, err := Parse([]byte(addon), "tool.toml"); err != nil {
		t.Fatalf("Parse(addon) error = %v", err)
	}
}

func TestParseRejectsInvalidDefinitions(t *testing.T) {
	header := func(kind string) string {
		return "schema = 1\nkind = \"" + kind + "\"\nname = \"x\"\nversion = \"1.0.0\"\ndescription = \"d\"\n"
	}
	preset := header("preset") + "[image]\nbase = \"debian\"\n"
	addon := header("addon")
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"syntax", "schema = ", "x.toml:1"},
		{"unknown field", preset + "colour = \"red\"\n", "unknown fields"},
		{"unknown nested field", preset + "[setup]\nscripts = \"x\"\n", "unknown fields"},
		{"schema", strings.Replace(preset, "schema = 1", "schema = 3", 1), "schema is 3"},
		{"kind", header("plugin"), `kind is "plugin"`},
		{"name", strings.Replace(preset, `name = "x"`, `name = "X"`, 1), `name "X" must match`},
		{"version", strings.Replace(preset, `"1.0.0"`, `"1.0"`, 1), "MAJOR.MINOR.PATCH"},
		{"description", strings.Replace(preset, `description = "d"`, `description = " "`, 1), "description is required"},
		{"preset without base", header("preset"), "image.base is required"},
		{"addon with base", addon + "[image]\nbase = \"debian\"\n", "image.base can only be set by a preset"},
		{"variant with base", addon + "[[variant]]\nwhen = { preset = [\"node\"] }\n[variant.image]\nbase = \"debian\"\n", "variant[0]: image.base cannot be set"},
		{"option without default", addon + "[options.v]\npattern = '[0-9]+'\n", "options.v: default is required"},
		{"option with both", addon + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\nchoices = [\"1\"]\n", "exactly one of choices or pattern"},
		{"option with neither", addon + "[options.v]\ndefault = \"1\"\n", "exactly one of choices or pattern"},
		{"option bad pattern", addon + "[options.v]\ndefault = \"1\"\npattern = '[0-9'\n", "invalid pattern"},
		{"option default mismatch", addon + "[options.v]\ndefault = \"x\"\npattern = '[0-9]+'\n", `"x" does not match`},
		{"option default not a choice", addon + "[options.v]\ndefault = \"c\"\nchoices = [\"a\", \"b\"]\n", `"c" is not one of a, b`},
		{"option unsafe choice", addon + "[options.v]\ndefault = \"a\"\nchoices = [\"a\", \"b;rm\"]\n", `choice "b;rm" must match`},
		{"option duplicate choice", addon + "[options.v]\ndefault = \"a\"\nchoices = [\"a\", \"a\"]\n", `choice "a" is duplicated`},
		{"option name", addon + "[options.Version]\ndefault = \"1\"\npattern = '1'\n", "option name must match"},
		{"apt package", addon + "[image]\napt = [\"bad package\"]\n", "not a valid apt package name"},
		{"multi-line run", addon + "[image]\nroot_run = [\"true\\nUSER root\"]\n", "single non-empty line"},
		{"empty run", addon + "[image]\nuser_run = [\" \"]\n", "single non-empty line"},
		{"feature value", addon + "[features.\"f:1\"]\nlist = [1]\n", "must be a string, boolean, or number"},
		{"relative path", addon + "[container]\npath = [\"bin\"]\n", `"bin" must be absolute`},
		{"reserved env", addon + "[container.env]\nPATH = \"/bin\"\n", "PATH is managed by projectsetup"},
		{"env name", addon + "[container.env]\n\"A-B\" = \"1\"\n", `"A-B" is not a valid variable name`},
		{"unknown option placeholder", addon + "[setup]\nscript = \"echo ${option:missing}\"\n", "no option \"missing\""},
		{"unknown project placeholder", addon + "[setup]\nscript = \"echo ${project:user}\"\n", "unknown placeholder ${project:user}"},
		{"reserved service", addon + "[services.app]\nimage = \"redis\"\n", "reserved for the app container"},
		{"service name", addon + "[services.Cache]\nimage = \"redis\"\n", "service name must match"},
		{"restart policy", addon + "[services.cache]\nimage = \"redis\"\nrestart = \"sometimes\"\n", "not a Compose restart policy"},
		{"depends on", addon + "[services.cache]\nimage = \"redis\"\napp_depends_on = \"service_ready\"\n", "app_depends_on"},
		{"volume path", addon + "[services.cache]\nimage = \"redis\"\nvolumes = { data = \"data\" }\n", "container path must be absolute"},
		{"volume name", addon + "[services.cache]\nimage = \"redis\"\nvolumes = { \"/host\" = \"/data\" }\n", "named volume \"/host\""},
		{"healthcheck test", addon + "[services.cache]\nimage = \"redis\"\n[services.cache.healthcheck]\ninterval = \"5s\"\n", "healthcheck.test is required"},
		{"empty condition", addon + "[[variant]]\nwhen = {}\n", "at least one condition is required"},
		{"condition option", addon + "[[variant]]\nwhen = { option = { missing = { in = [\"1\"] } } }\n", "has no option \"missing\""},
		{"detect in addon", addon + "[detect]\nsignals = [\"x\"]\n", "detect: detection is only supported in presets"},
		{"option detect in addon", addon + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\n[options.v.detect]\nsources = [{ file = \".v\" }]\n", "options.v.detect: detection is only supported in presets"},
		{"absolute signal", preset + "[detect]\nsignals = [\"/etc/passwd\"]\n", "must be a clean path inside the project"},
		{"parent signal", preset + "[detect]\nsignals = [\"../x\"]\n", "must be a clean path inside the project"},
		{"unclean signal", preset + "[detect]\nsignals = [\"a//b\"]\n", "must be a clean path inside the project"},
		{"nested glob", preset + "[detect]\nsignals = [\"lib/*.rb\"]\n", "cannot include a directory"},
		{"bad glob", preset + "[detect]\nsignals = [\"[\"]\n", "invalid glob"},
		{"match pattern", preset + "[detect]\nmatch = [{ file = \"x\", pattern = \"(\" }]\n", "detect.match[0]: invalid pattern"},
		{"match without file", preset + "[detect]\nmatch = [{ pattern = \"x\" }]\n", "detect.match[0]: file is required"},
		{"supersedes", preset + "[detect]\nsupersedes = [\"Ruby\"]\n", `detect.supersedes: "Ruby"`},
		{"suggest without match", preset + "[[detect.suggest]]\naddon = \"postgres\"\n", "detect.suggest[0].match: at least one file rule is required"},
		{"suggest addon", preset + "[[detect.suggest]]\naddon = \"\"\nmatch = [{ file = \"x\" }]\n", "detect.suggest[0].addon"},
		{"warning message", preset + "[[detect.warning]]\nmatch = [{ file = \"x\" }]\n", "detect.warning[0].message is required"},
		{"source file and builtin", preset + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\n[options.v.detect]\nsources = [{ file = \"x\", builtin = \"package-json-engines\" }]\n", "sources[0]: set exactly one of file or builtin"},
		{"source builtin", preset + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\n[options.v.detect]\nsources = [{ builtin = \"cargo\" }]\n", `unknown builtin "cargo" (available: package-json-engines)`},
		{"source builtin pattern", preset + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\n[options.v.detect]\nsources = [{ builtin = \"package-json-engines\", pattern = \"(x)\" }]\n", "pattern requires file"},
		{"source capture group", preset + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\n[options.v.detect]\nsources = [{ file = \"x\", pattern = \"v[0-9]+\" }]\n", "needs a capture group"},
		{"sources and choices", preset + "[options.v]\ndefault = \"a\"\nchoices = [\"a\"]\n[options.v.detect]\nsources = [{ file = \"x\" }]\nchoices = { a = [{ file = \"y\" }] }\n", "set sources or choices, not both"},
		{"choices without option choices", preset + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\n[options.v.detect.choices]\n1 = [{ file = \"y\" }]\n", "the option has no choices; use sources"},
		{"unknown detected choice", preset + "[options.v]\ndefault = \"a\"\nchoices = [\"a\"]\n[options.v.detect.choices]\nb = [{ file = \"y\" }]\n", `"b" is not a choice of the option`},
		{"empty detected choice", preset + "[options.v]\ndefault = \"a\"\nchoices = [\"a\"]\n[options.v.detect.choices]\na = []\n", "choices.a: at least one file rule is required"},
		{"condition option criteria", addon + "[options.v]\ndefault = \"1\"\npattern = '[0-9]+'\n[[variant]]\nwhen = { option = { v = {} } }\n", "set in or below"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.source), "x.toml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %v, want it to contain %q", err, tt.want)
			}
			if !strings.HasPrefix(err.Error(), "x.toml") {
				t.Fatalf("Parse() error = %v, want it to name the file", err)
			}
		})
	}
}

func TestParseAggregatesProblems(t *testing.T) {
	_, err := Parse([]byte("schema = 1\nkind = \"addon\"\nname = \"Bad\"\nversion = \"1\"\ndescription = \"\"\n"), "bad.toml")
	if err == nil {
		t.Fatal("Parse() error = nil")
	}
	for _, want := range []string{"name \"Bad\"", "MAJOR.MINOR.PATCH", "description is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Parse() error = %v, want it to contain %q", err, want)
		}
	}
}

func mustParse(t *testing.T, source string) Definition {
	t.Helper()
	definition, err := Parse([]byte(source), "test.toml")
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func testRegistry(t *testing.T, sources ...string) *Registry {
	t.Helper()
	var definitions []Definition
	for _, source := range sources {
		definitions = append(definitions, mustParse(t, source))
	}
	registry, err := NewRegistry(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

const languagePreset = `
schema = 1
kind = "preset"
name = "lang"
version = "1.0.0"
description = "Language"

[options.version]
default = "2"
pattern = '[0-9]+'

[image]
base = "debian:trixie"
apt = ["make"]
root_run = ["echo ${project:name}"]
user_run = ["echo ${option:version}"]

[features."lang:1"]
version = "${option:version}"
fast = true

[container]
path = ["${project:home}/.lang/bin"]
env = { LANG_HOME = "${project:home}/.lang", LANG_PROJECT = "${project:workspace}" }

[setup]
script = 'lang install "${HOME}" ${option:version}'

[[variant]]
when = { option = { version = { below = 2 } } }
[variant.features."lang:1"]
legacy = true

[[variant]]
when = { addon = ["db"] }
[variant.image]
apt = ["db-client"]
`

const databaseAddon = `
schema = 1
kind = "addon"
name = "db"
version = "1.0.0"
description = "Database"

[options.version]
default = "5"
choices = ["4", "5"]

[container.env]
DB_NAME = "${project:name}"

[services.db]
image = "db:${option:version}"
restart = "unless-stopped"
app_depends_on = "service_healthy"
volumes = { db-data = "/data" }
environment = { DB_NAME = "${project:name}" }
[services.db.healthcheck]
test = ["CMD", "ping", "${project:name}"]
retries = 3

[[variant]]
when = { not_preset = ["other"], option = { version = { in = ["4"] } } }
[variant.services.db]
image = "legacy-db:${option:version}"
volumes = { db-data = "/var/data" }
environment = { DB_MODE = "legacy" }
`

func TestResolveMergesAndExpands(t *testing.T) {
	registry := testRegistry(t, languagePreset, databaseAddon)
	project := Project{Name: "demo", Home: "/home/vscode", Workspace: "/workspaces/demo"}

	got, err := registry.Resolve(Selection{Preset: "lang", Addons: []string{"db", "db"}, Project: project})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := Resolved{
		Base:     "debian:trixie",
		Apt:      [][]string{{"make"}, {"db-client"}},
		RootRun:  []string{"echo demo"},
		UserRun:  []string{"echo 2"},
		Features: map[string]map[string]any{"lang:1": {"version": "2", "fast": true}},
		Path:     []string{"/home/vscode/.lang/bin"},
		Env:      map[string]string{"LANG_HOME": "/home/vscode/.lang", "LANG_PROJECT": "/workspaces/demo", "DB_NAME": "demo"},
		Setup:    []string{`lang install "${HOME}" 2`},
		Services: map[string]Service{"db": {
			Image: "db:5", Restart: "unless-stopped", AppDependsOn: "service_healthy",
			Volumes:     map[string]string{"db-data": "/data"},
			Environment: map[string]string{"DB_NAME": "demo"},
			Healthcheck: &Healthcheck{Test: []string{"CMD", "ping", "demo"}, Retries: 3},
		}},
		Options: map[string]map[string]string{"lang": {"version": "2"}, "db": {"version": "5"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() =\n%#v\nwant\n%#v", got, want)
	}

	got, err = registry.Resolve(Selection{
		Preset: "lang", Addons: []string{"db"}, Project: project,
		Options: map[string]map[string]string{"lang": {"version": "1"}, "db": {"version": "4"}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if feature := got.Features["lang:1"]; feature["legacy"] != true || feature["version"] != "1" {
		t.Errorf("variant did not extend the feature: %v", feature)
	}
	service := got.Services["db"]
	if service.Image != "legacy-db:4" || service.Volumes["db-data"] != "/var/data" || service.Environment["DB_MODE"] != "legacy" || service.Environment["DB_NAME"] != "demo" || service.Restart != "unless-stopped" {
		t.Errorf("variant did not override the service: %+v", service)
	}

	got, err = registry.Resolve(Selection{Preset: "lang", Project: project})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(got.Apt) != 1 || len(got.Services) != 0 || got.Env["DB_NAME"] != "" {
		t.Errorf("unselected add-on contributed: %+v", got)
	}
}

func TestResolveDoesNotModifyDefinitions(t *testing.T) {
	registry := testRegistry(t, languagePreset, databaseAddon)
	selection := Selection{Preset: "lang", Addons: []string{"db"}, Project: Project{Name: "one"},
		Options: map[string]map[string]string{"lang": {"version": "1"}, "db": {"version": "4"}}}
	if _, err := registry.Resolve(selection); err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Lookup("lang")
	if definition.Features["lang:1"]["version"] != "${option:version}" || definition.Features["lang:1"]["legacy"] != nil {
		t.Fatalf("Resolve() modified the definition: %v", definition.Features)
	}
	addon, _ := registry.Lookup("db")
	if addon.Services["db"].Image != "db:${option:version}" {
		t.Fatalf("Resolve() modified the add-on: %v", addon.Services)
	}
}

func TestResolveRejectsInvalidSelections(t *testing.T) {
	conflicting := `
schema = 1
kind = "addon"
name = "other-db"
version = "1.0.0"
description = "Conflicts with db"
[container.env]
DB_NAME = "x"
[features."lang:1"]
[services.db]
image = "x"
`
	sharedVolume := `
schema = 1
kind = "addon"
name = "cache"
version = "1.0.0"
description = "Shares a volume"
[services.cache]
image = "cache"
volumes = { db-data = "/cache" }
`
	noImage := `
schema = 1
kind = "addon"
name = "broken"
version = "1.0.0"
description = "Service without an image"
[services.broken]
restart = "always"
`
	registry := testRegistry(t, languagePreset, databaseAddon, conflicting, sharedVolume, noImage)
	tests := []struct {
		name      string
		selection Selection
		want      []string
	}{
		{"unknown preset", Selection{Preset: "missing"}, []string{`unknown preset "missing" (available: lang)`}},
		{"add-on as preset", Selection{Preset: "db"}, []string{`unknown preset "db"`}},
		{"unknown add-on", Selection{Preset: "lang", Addons: []string{"missing"}}, []string{`unknown add-on "missing" (available: broken, cache, db, other-db)`}},
		{"unknown option", Selection{Preset: "lang", Options: map[string]map[string]string{"lang": {"size": "1"}}}, []string{`lang has no option "size" (available: version)`}},
		{"invalid option value", Selection{Preset: "lang", Options: map[string]map[string]string{"lang": {"version": "x"}}}, []string{"option lang.version:"}},
		{"injected option value", Selection{Preset: "lang", Options: map[string]map[string]string{"lang": {"version": "1;rm"}}}, []string{"option lang.version:"}},
		{"unselected options", Selection{Preset: "lang", Options: map[string]map[string]string{"db": {"version": "4"}}}, []string{`options are set for "db", which is not selected`}},
		{"conflicts", Selection{Preset: "lang", Addons: []string{"db", "other-db"}}, []string{
			`containerEnv "DB_NAME" is set by both db and other-db`,
			`feature "lang:1" is set by both lang and other-db`,
			`service "db" is set by both db and other-db`,
		}},
		{"shared volume", Selection{Preset: "lang", Addons: []string{"cache", "db"}}, []string{`named volume "db-data" is used by both services "cache" and "db"`}},
		{"service without image", Selection{Preset: "lang", Addons: []string{"broken"}}, []string{`service "broken" has no image`}},
		{"path placeholder", Selection{Preset: "lang", Project: Project{Home: "relative"}}, []string{`container.path entry "relative/.lang/bin" must be an absolute path`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.selection.Project.Home == "" {
				tt.selection.Project = Project{Name: "demo", Home: "/home/vscode", Workspace: "/workspaces/demo"}
			}
			_, err := registry.Resolve(tt.selection)
			if err == nil {
				t.Fatalf("Resolve() error = nil, want %q", tt.want)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Resolve() error = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestNewRegistryRejectsDuplicateAndInvalidDefinitions(t *testing.T) {
	definition := mustParse(t, minimalPreset)
	if _, err := NewRegistry(definition, definition); err == nil || !strings.Contains(err.Error(), `"base" is defined more than once`) {
		t.Fatalf("NewRegistry() error = %v, want duplicate rejection", err)
	}
	definition.Version = "1"
	if _, err := NewRegistry(definition); err == nil || !strings.Contains(err.Error(), "MAJOR.MINOR.PATCH") {
		t.Fatalf("NewRegistry() error = %v, want validation", err)
	}
}

func TestConditionMatches(t *testing.T) {
	below := 18
	tests := []struct {
		name      string
		condition Condition
		preset    string
		addons    []string
		options   map[string]string
		want      bool
	}{
		{"preset match", Condition{Preset: []string{"node", "rails"}}, "rails", nil, nil, true},
		{"preset mismatch", Condition{Preset: []string{"node"}}, "rails", nil, nil, false},
		{"not preset", Condition{NotPreset: []string{"rails"}}, "rails", nil, nil, false},
		{"not other preset", Condition{NotPreset: []string{"rails"}}, "node", nil, nil, true},
		{"all add-ons", Condition{Addon: []string{"a", "b"}}, "node", []string{"a", "b", "c"}, nil, true},
		{"missing add-on", Condition{Addon: []string{"a", "b"}}, "node", []string{"a"}, nil, false},
		{"option in", Condition{Option: map[string]OptionCondition{"v": {In: []string{"1", "2"}}}}, "node", nil, map[string]string{"v": "2"}, true},
		{"option not in", Condition{Option: map[string]OptionCondition{"v": {In: []string{"1"}}}}, "node", nil, map[string]string{"v": "2"}, false},
		{"below", Condition{Option: map[string]OptionCondition{"v": {Below: &below}}}, "node", nil, map[string]string{"v": "17"}, true},
		{"below with minor", Condition{Option: map[string]OptionCondition{"v": {Below: &below}}}, "node", nil, map[string]string{"v": "9.6"}, true},
		{"not below", Condition{Option: map[string]OptionCondition{"v": {Below: &below}}}, "node", nil, map[string]string{"v": "18"}, false},
		{"below needs a number", Condition{Option: map[string]OptionCondition{"v": {Below: &below}}}, "node", nil, map[string]string{"v": "latest"}, false},
		{"all criteria", Condition{Preset: []string{"node"}, Addon: []string{"a"}, Option: map[string]OptionCondition{"v": {In: []string{"1"}}}}, "node", []string{"a"}, map[string]string{"v": "1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.condition.matches(tt.preset, tt.addons, tt.options); got != tt.want {
				t.Fatalf("matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuiltinDefinitions(t *testing.T) {
	registry := Builtin()
	if got, want := registry.Names(KindPreset), []string{"node", "python", "rails", "ruby"}; !reflect.DeepEqual(got, want) {
		t.Errorf("built-in presets = %v, want %v", got, want)
	}
	if got, want := registry.Names(KindAddon), []string{"go", "postgres", "redis", "rust", "sqlite"}; !reflect.DeepEqual(got, want) {
		t.Errorf("built-in add-ons = %v, want %v", got, want)
	}
	project := Project{Name: "demo", Home: "/home/vscode", Workspace: "/workspaces/demo"}
	for _, preset := range registry.Names(KindPreset) {
		for _, addons := range [][]string{nil, {"postgres"}, {"sqlite"}, {"go"}, {"rust"}, {"redis"}, {"go", "postgres", "redis", "rust", "sqlite"}} {
			if _, err := registry.Resolve(Selection{Preset: preset, Addons: addons, Project: project}); err != nil {
				t.Errorf("Resolve(%s, %v) error = %v", preset, addons, err)
			}
		}
	}
}

func TestBuiltinPostgresImageAndDataPath(t *testing.T) {
	tests := []struct {
		version   string
		wantImage string
		wantPath  string
	}{
		{"16", "postgres:16-bookworm", "/var/lib/postgresql/data"},
		{"17", "postgres:17-bookworm", "/var/lib/postgresql/data"},
		{"18", "postgres:18-trixie", "/var/lib/postgresql"},
		{"19", "postgres:19-trixie", "/var/lib/postgresql"},
	}
	for _, tt := range tests {
		resolved, err := Builtin().Resolve(Selection{
			Preset: "node", Addons: []string{"postgres"},
			Options: map[string]map[string]string{"postgres": {"version": tt.version}},
			Project: Project{Name: "demo", Home: "/home/vscode", Workspace: "/workspaces/demo"},
		})
		if err != nil {
			t.Fatalf("Resolve(postgres %s) error = %v", tt.version, err)
		}
		service := resolved.Services["postgres"]
		if service.Image != tt.wantImage || service.Volumes["postgres-data"] != tt.wantPath {
			t.Errorf("postgres %s: image %q volume %q, want %q %q", tt.version, service.Image, service.Volumes["postgres-data"], tt.wantImage, tt.wantPath)
		}
	}
}
