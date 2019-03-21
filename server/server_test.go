// Copyright 2017 HootSuite Media Inc.
//
// Licensed under the Apache License, Version 2.0 (the License);
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an AS IS BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// Modified hereafter by contributors to runatlantis/atlantis.

package server_test

import (
	"bytes"
	"errors"
	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/yaml/valid"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	. "github.com/petergtz/pegomock"
	"github.com/runatlantis/atlantis/server"
	"github.com/runatlantis/atlantis/server/events/locking/mocks"
	"github.com/runatlantis/atlantis/server/events/models"
	sMocks "github.com/runatlantis/atlantis/server/mocks"
	. "github.com/runatlantis/atlantis/testing"
)

func TestNewServer(t *testing.T) {
	t.Log("Run through NewServer constructor")
	tmpDir, err := ioutil.TempDir("", "")
	Ok(t, err)
	_, err = server.NewServer(server.UserConfig{
		DataDir:     tmpDir,
		AtlantisURL: "http://example.com",
	}, server.Config{})
	Ok(t, err)
}

// todo: test what happens if we set different flags. The generated config should be different.
func TestRepoConfig(t *testing.T) {
	t.SkipNow()
	tmpDir, err := ioutil.TempDir("", "")
	Ok(t, err)

	repoYaml := `
repos:
- id: "https://github.com/runatlantis/atlantis"
`
	expConfig := valid.GlobalCfg{
		Repos: []valid.Repo{
			{
				ID: "https://github.com/runatlantis/atlantis",
			},
		},
		Workflows: map[string]valid.Workflow{},
	}
	repoFileLocation := filepath.Join(tmpDir, "repos.yaml")
	err = ioutil.WriteFile(repoFileLocation, []byte(repoYaml), 0600)
	Ok(t, err)

	s, err := server.NewServer(server.UserConfig{
		DataDir:     tmpDir,
		RepoConfig:  repoFileLocation,
		AtlantisURL: "http://example.com",
	}, server.Config{})
	Ok(t, err)
	Equals(t, expConfig, s.CommandRunner.ProjectCommandBuilder.(*events.DefaultProjectCommandBuilder).GlobalCfg)
}

func TestNewServer_InvalidAtlantisURL(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	Ok(t, err)
	_, err = server.NewServer(server.UserConfig{
		DataDir:     tmpDir,
		AtlantisURL: "example.com",
	}, server.Config{
		AtlantisURLFlag: "atlantis-url",
	})
	ErrEquals(t, "parsing --atlantis-url flag \"example.com\": http or https must be specified", err)
}

func TestIndex_LockErr(t *testing.T) {
	t.Log("index should return a 503 if unable to list locks")
	RegisterMockTestingT(t)
	l := mocks.NewMockLocker()
	When(l.List()).ThenReturn(nil, errors.New("err"))
	s := server.Server{
		Locker: l,
	}
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	w := httptest.NewRecorder()
	s.Index(w, req)
	responseContains(t, w, 503, "Could not retrieve locks: err")
}

func TestIndex_Success(t *testing.T) {
	t.Log("Index should render the index template successfully.")
	RegisterMockTestingT(t)
	l := mocks.NewMockLocker()
	// These are the locks that we expect to be rendered.
	now := time.Now()
	locks := map[string]models.ProjectLock{
		"lkysow/atlantis-example/./default": {
			Pull: models.PullRequest{
				Num: 9,
			},
			Project: models.Project{
				RepoFullName: "lkysow/atlantis-example",
			},
			Time: now,
		},
	}
	When(l.List()).ThenReturn(locks, nil)
	it := sMocks.NewMockTemplateWriter()
	r := mux.NewRouter()
	atlantisVersion := "0.3.1"
	// Need to create a lock route since the server expects this route to exist.
	r.NewRoute().Path("/lock").
		Queries("id", "{id}").Name(server.LockViewRouteName)
	u, err := url.Parse("https://example.com")
	Ok(t, err)
	s := server.Server{
		Locker:          l,
		IndexTemplate:   it,
		Router:          r,
		AtlantisVersion: atlantisVersion,
		AtlantisURL:     u,
	}
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	w := httptest.NewRecorder()
	s.Index(w, req)
	it.VerifyWasCalledOnce().Execute(w, server.IndexData{
		Locks: []server.LockIndexData{
			{
				LockPath:     "/lock?id=lkysow%252Fatlantis-example%252F.%252Fdefault",
				RepoFullName: "lkysow/atlantis-example",
				PullNum:      9,
				Time:         now,
			},
		},
		AtlantisVersion: atlantisVersion,
	})
	responseContains(t, w, http.StatusOK, "")
}

func TestHealthz(t *testing.T) {
	s := server.Server{}
	req, _ := http.NewRequest("GET", "/healthz", bytes.NewBuffer(nil))
	w := httptest.NewRecorder()
	s.Healthz(w, req)
	Equals(t, http.StatusOK, w.Result().StatusCode)
	body, _ := ioutil.ReadAll(w.Result().Body)
	Equals(t, "application/json", w.Result().Header["Content-Type"][0])
	Equals(t,
		`{
  "status": "ok"
}`, string(body))
}

func TestParseAtlantisURL(t *testing.T) {
	cases := []struct {
		In     string
		ExpErr string
		ExpURL string
	}{
		// Valid URLs should work.
		{
			In:     "https://example.com",
			ExpURL: "https://example.com",
		},
		{
			In:     "http://example.com",
			ExpURL: "http://example.com",
		},
		{
			In:     "http://example.com/",
			ExpURL: "http://example.com",
		},
		{
			In:     "http://example.com",
			ExpURL: "http://example.com",
		},
		{
			In:     "http://example.com:4141",
			ExpURL: "http://example.com:4141",
		},
		{
			In:     "http://example.com:4141/",
			ExpURL: "http://example.com:4141",
		},
		{
			In:     "http://example.com/baseurl",
			ExpURL: "http://example.com/baseurl",
		},
		{
			In:     "http://example.com/baseurl/",
			ExpURL: "http://example.com/baseurl",
		},
		{
			In:     "http://example.com/baseurl/test",
			ExpURL: "http://example.com/baseurl/test",
		},

		// Must be valid URL.
		{
			In:     "::",
			ExpErr: "parse ::: missing protocol scheme",
		},

		// Must be absolute.
		{
			In:     "/hi",
			ExpErr: "http or https must be specified",
		},

		// Must have http or https scheme..
		{
			In:     "localhost/test",
			ExpErr: "http or https must be specified",
		},
		{
			In:     "httpl://localhost/test",
			ExpErr: "http or https must be specified",
		},
	}

	for _, c := range cases {
		t.Run(c.In, func(t *testing.T) {
			act, err := server.ParseAtlantisURL(c.In)
			if c.ExpErr != "" {
				ErrEquals(t, c.ExpErr, err)
			} else {
				Ok(t, err)
				Equals(t, c.ExpURL, act.String())
			}
		})
	}
}

func responseContains(t *testing.T, r *httptest.ResponseRecorder, status int, bodySubstr string) {
	t.Helper()
	body, err := ioutil.ReadAll(r.Result().Body)
	Ok(t, err)
	Assert(t, status == r.Result().StatusCode, "exp %d got %d, body: %s", status, r.Result().StatusCode, string(body))
	Assert(t, strings.Contains(string(body), bodySubstr), "exp %q to be contained in %q", bodySubstr, string(body))
}
