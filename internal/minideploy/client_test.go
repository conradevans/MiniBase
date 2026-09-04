package minideploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef01234567"

func TestLifecycleClientUsesBearerAuthAndSafePayloads(
	t *testing.T,
) {
	var requests int

	server := httptest.NewServer(
		http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				requests++

				if r.Header.Get(
					"Authorization",
				) != "Bearer "+testToken {

					t.Fatalf(
						"Authorization = %q",
						r.Header.Get("Authorization"),
					)
				}

				switch r.URL.Path {
				case "/internal/minibase/deployments":
					if r.Method != http.MethodGet {
						t.Fatalf(
							"method = %s",
							r.Method,
						)
					}

					_ = json.NewEncoder(w).Encode(
						[]Deployment{{
							App:       "scheduler",
							Supported: true,
							Status:    "running",
						}},
					)

				case "/internal/minibase/deployments/scheduler/database/detach":
					var input struct {
						DatabaseID   string `json:"databaseId"`
						AttachmentID string `json:"attachmentId"`
					}

					if err := json.NewDecoder(
						r.Body,
					).Decode(&input); err != nil {

						t.Fatal(err)
					}

					if input.DatabaseID !=
						"database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
						input.AttachmentID !=
							"attachment_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {

						t.Fatalf(
							"detach input = %#v",
							input,
						)
					}

					w.WriteHeader(
						http.StatusNoContent,
					)

				case "/internal/minibase/deployments/scheduler/database/attach":
					var input struct {
						DatabaseID string `json:"databaseId"`
					}

					if err := json.NewDecoder(
						r.Body,
					).Decode(&input); err != nil {

						t.Fatal(err)
					}

					if input.DatabaseID !=
						"database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {

						t.Fatalf(
							"attach input = %#v",
							input,
						)
					}

					w.WriteHeader(
						http.StatusNoContent,
					)

				default:
					t.Fatalf(
						"unexpected path %q",
						r.URL.Path,
					)
				}
			},
		),
	)
	defer server.Close()

	client, err := NewClient(
		server.URL,
		[]byte(testToken),
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}

	deployments, err :=
		client.ListDeployments(
			context.Background(),
		)
	if err != nil {
		t.Fatal(err)
	}

	if len(deployments) != 1 ||
		deployments[0].App != "scheduler" {

		t.Fatalf(
			"deployments = %#v",
			deployments,
		)
	}

	if err := client.DetachDatabase(
		context.Background(),
		"scheduler",
		"database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"attachment_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	); err != nil {
		t.Fatal(err)
	}

	if err := client.AttachDatabase(
		context.Background(),
		"scheduler",
		"database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	); err != nil {
		t.Fatal(err)
	}

	if requests != 3 {
		t.Fatalf(
			"requests = %d; want 3",
			requests,
		)
	}
}

func TestLifecycleClientRejectsNonLoopbackURL(
	t *testing.T,
) {
	if _, err := NewClient(
		"http://example.com:9000",
		[]byte(testToken),
		nil,
	); err == nil {
		t.Fatal(
			"non-loopback MiniDeploy URL was accepted",
		)
	}
}
