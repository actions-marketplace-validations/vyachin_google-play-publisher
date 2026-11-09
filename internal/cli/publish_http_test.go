package cli_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
)

func TestPublishCanceledHTTPCommit(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		t.Run(strconv.FormatBool(jsonOutput), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			const application = "/androidpublisher/v3/applications/com.example.app"
			const edit = application + "/edits/edit-1"
			const commit = "POST " + edit + ":commit"
			var mu sync.Mutex
			var requests []string
			releaseCommit := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request := r.Method + " " + r.URL.Path
				mu.Lock()
				requests = append(requests, request)
				mu.Unlock()
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Errorf("read request %s: %v", request, err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch request {
				case "GET " + application + "/tracks/internal/releases":
					fmt.Fprint(w, `{"releases":[]}`)
				case "POST " + application + "/edits":
					fmt.Fprint(w, `{"id":"edit-1"}`)
				case "POST /upload" + edit + "/bundles":
					fmt.Fprint(w, `{"versionCode":42}`)
				case "PUT " + edit + "/tracks/internal":
					fmt.Fprint(w, `{"track":"internal"}`)
				case commit:
					// Play has received the complete request. Interrupt the client
					// while withholding the response until the CLI has returned.
					cancel()
					<-releaseCommit
				case "DELETE " + edit:
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request: %s", request)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			// The commit handler blocks until releaseCommit is closed. Release
			// it first, otherwise Server.Close waits for the handler forever.
			t.Cleanup(func() {
				close(releaseCommit)
				server.Close()
			})

			api, err := play.New(ctx, play.Options{
				HTTPClient: server.Client(), Endpoint: server.URL,
				Retry: play.RetryOptions{Attempts: 1},
			})
			if err != nil {
				t.Fatalf("create Play adapter: %v", err)
			}
			args := []string{"publish", "-f", writeAAB(t)}
			if jsonOutput {
				args = append(args, "--json")
			}
			out, errOut, code := runWithAPIAndContext(ctx, t, api, args...)
			if code != int(exitcode.Interrupted) {
				t.Fatalf("code = %d, want 130: %s", code, errOut)
			}
			if !strings.Contains(errOut, "commit state is unknown") || !strings.Contains(errOut, "edit-1") || !strings.Contains(errOut, "check the track") {
				t.Fatalf("missing recovery diagnostic: %s", errOut)
			}
			if jsonOutput {
				object := decodeSingleObject(t, out)
				detail := errorObject(t, object)
				if object["ok"] != false || detail["code"] != "interrupted" || detail["exitCode"] != float64(exitcode.Interrupted) {
					t.Fatalf("unexpected JSON error: %v", object)
				}
				if !strings.Contains(detail["message"].(string), "edit-1") || !strings.Contains(detail["hint"].(string), "check the track") {
					t.Fatalf("missing JSON recovery diagnostic: %v", detail)
				}
			} else if out != "" {
				t.Fatalf("canceled commit printed success: %s", out)
			}

			want := []string{
				"GET " + application + "/tracks/internal/releases",
				"POST " + application + "/edits",
				"POST /upload" + edit + "/bundles",
				"PUT " + edit + "/tracks/internal",
				commit,
			}
			mu.Lock()
			got := append([]string(nil), requests...)
			mu.Unlock()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected HTTP calls (no retry, reconciliation or deletion is allowed after cancellation):\ngot  %v\nwant %v", got, want)
			}
		})
	}
}
