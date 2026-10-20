package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseSourceDefaultsToGitHub(t *testing.T) {
	t.Setenv(releaseAPIBaseURLEnv, "")
	t.Setenv(releaseDownloadBaseURLEnv, "")
	t.Setenv(releaseRepoEnv, "")

	if got := releaseAPIBaseURL(); got != defaultReleaseAPIBaseURL {
		t.Fatalf("release API base URL = %q, want %q", got, defaultReleaseAPIBaseURL)
	}
	if got := releaseRepo(); got != defaultReleaseRepo {
		t.Fatalf("release repo = %q, want %q", got, defaultReleaseRepo)
	}
	if got := releaseAssetDownloadURL("archive.tar.gz", "v1.2.3", "https://github.example/archive.tar.gz"); got != "https://github.example/archive.tar.gz" {
		t.Fatalf("fallback download URL = %q", got)
	}
}

func TestReleaseSourceOverridesGitHub(t *testing.T) {
	t.Setenv(releaseAPIBaseURLEnv, "https://mirror.example/api/")
	t.Setenv(releaseDownloadBaseURLEnv, "https://mirror.example/releases/")
	t.Setenv(releaseRepoEnv, "custom-owner/custom-repo")

	if got := releaseAPIBaseURL(); got != "https://mirror.example/api" {
		t.Fatalf("release API base URL = %q", got)
	}
	if got := releaseRepo(); got != "custom-owner/custom-repo" {
		t.Fatalf("release repo = %q, want custom-owner/custom-repo", got)
	}
	if got := releaseAssetDownloadURL("multica-cli-1.2.3-linux-amd64.tar.gz", "v1.2.3", "https://github.example/archive.tar.gz"); got != "https://mirror.example/releases/v1.2.3/multica-cli-1.2.3-linux-amd64.tar.gz" {
		t.Fatalf("mirror download URL = %q", got)
	}
}

func TestFetchLatestReleaseUsesConfiguredAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/16Janis12/multica/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GitHubRelease{TagName: "v9.8.7"})
	}))
	defer server.Close()
	t.Setenv(releaseAPIBaseURLEnv, server.URL)
	t.Setenv(releaseRepoEnv, "")

	got, err := FetchLatestRelease()
	if err != nil {
		t.Fatalf("FetchLatestRelease() error = %v", err)
	}
	if got.TagName != "v9.8.7" {
		t.Fatalf("release tag = %q, want v9.8.7", got.TagName)
	}
}
