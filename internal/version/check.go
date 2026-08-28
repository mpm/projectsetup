package version

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	GitHubRepo   = "mpm/projectsetup"
	checkURL     = "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
	checkTimeout = 5 * time.Second
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckResult describes the latest published release relative to this binary.
type CheckResult struct {
	Current         string
	Latest          string
	UpdateAvailable bool
	ReleaseURL      string
}

// CheckForUpdate queries GitHub for the latest release. Failures are ignored so
// update checks never interfere with the requested command.
func CheckForUpdate() *CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	return CheckForUpdateWithContext(ctx)
}

// CheckForUpdateWithContext performs an update check with the supplied context.
func CheckForUpdateWithContext(ctx context.Context) *CheckResult {
	return checkForUpdate(ctx, http.DefaultClient, checkURL)
}

func checkForUpdate(ctx context.Context, client *http.Client, url string) *CheckResult {
	if IsDev() {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "projectsetup/"+Short())

	response, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}

	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil || release.TagName == "" {
		return nil
	}

	return &CheckResult{
		Current:         Version,
		Latest:          release.TagName,
		UpdateAvailable: isNewer(release.TagName, Version),
		ReleaseURL:      release.HTMLURL,
	}
}

func isNewer(latest, current string) bool {
	latestParts, ok := parseVersion(latest)
	if !ok {
		return false
	}
	currentParts, ok := parseVersion(current)
	if !ok {
		return false
	}
	for i := range latestParts {
		if latestParts[i] != currentParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	return false
}

func parseVersion(value string) ([3]int, bool) {
	var result [3]int
	value = strings.TrimPrefix(value, "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) == 0 || len(parts) > len(result) {
		return result, false
	}
	for i, part := range parts {
		if part == "" {
			return result, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return result, false
		}
		result[i] = n
	}
	return result, true
}
