package asimut

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLogin_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/public/login.php":
			http.SetCookie(w, &http.Cookie{
				Name:  "PHPSESSID",
				Value: "test-session-id",
			})
			w.WriteHeader(http.StatusFound)
		case "/services/v2/eventdefault":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"eventdefault": map[string]interface{}{
						"events": []interface{}{
							map[string]interface{}{
								"pe": []interface{}{
									map[string]interface{}{"id": 965, "ro": 1, "dn": "Teiln: Test User ([BM]Ob956)"},
								},
							},
						},
					},
					"success": true,
				},
			})
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test@example.com", "password123")
	err := client.Login()
	if err != nil {
		t.Fatalf("Login() returned error: %v", err)
	}

	if !client.LoggedIn() {
		t.Error("expected LoggedIn() to return true after successful login")
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/public/login.php":
			w.WriteHeader(http.StatusFound)
		case "/services/v2/eventdefault":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"success": false,
				},
			})
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad@example.com", "wrongpassword")
	err := client.Login()
	if err == nil {
		t.Fatal("Login() should return error for invalid credentials")
	}

	if client.LoggedIn() {
		t.Error("expected LoggedIn() to return false after failed login")
	}
}

func TestLogin_SessionExpired_RetriesWithFreshJar(t *testing.T) {
	loginAttempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/public/login.php":
			loginAttempts++
			http.SetCookie(w, &http.Cookie{
				Name:  "PHPSESSID",
				Value: "new-session-id",
			})
			w.WriteHeader(http.StatusFound)
		case "/services/v2/eventdefault":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"eventdefault": map[string]interface{}{
						"events": []interface{}{
							map[string]interface{}{
								"pe": []interface{}{
									map[string]interface{}{"id": 965, "ro": 1, "dn": "Teiln: Test User ([BM]Ob956)"},
								},
							},
						},
					},
					"success": true,
				},
			})
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test@example.com", "password123")

	// First login succeeds
	err := client.Login()
	if err != nil {
		t.Fatalf("first Login() returned error: %v", err)
	}

	// Simulate session expiry — invalidate and login again
	client.InvalidateSession()
	loginAttempts = 0

	err = client.Login()
	if err != nil {
		t.Fatalf("second Login() returned error: %v", err)
	}

	if !client.LoggedIn() {
		t.Error("expected LoggedIn() to return true after re-login")
	}
}

func TestLogin_FirstAttemptFails_RetrySucceeds(t *testing.T) {
	eventdefaultCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/public/login.php":
			http.SetCookie(w, &http.Cookie{
				Name:  "PHPSESSID",
				Value: "session-id",
			})
			w.WriteHeader(http.StatusFound)
		case "/services/v2/eventdefault":
			eventdefaultCalls++
			w.Header().Set("Content-Type", "application/json")
			// First call fails (simulates stale session), second succeeds
			if eventdefaultCalls > 1 {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"eventdefault": map[string]interface{}{
							"events": []interface{}{
								map[string]interface{}{
									"pe": []interface{}{
										map[string]interface{}{"id": 965, "ro": 1, "dn": "Teiln: Test User ([BM]Ob956)"},
									},
								},
							},
						},
						"success": true,
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"response": map[string]interface{}{
						"success": false,
					},
				})
			}
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test@example.com", "password123")
	err := client.Login()
	if err != nil {
		t.Fatalf("Login() returned error: %v (eventdefault calls: %d)", err, eventdefaultCalls)
	}

	if !client.LoggedIn() {
		t.Error("expected LoggedIn() to return true after retry succeeded")
	}

	if eventdefaultCalls < 2 {
		t.Errorf("expected at least 2 eventdefault calls (retry), got %d", eventdefaultCalls)
	}
}

func TestBookRoom_Success(t *testing.T) {
	expectedEventID := 12345
	loginDone := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/public/login.php":
			http.SetCookie(w, &http.Cookie{
				Name:  "PHPSESSID",
				Value: "test-session-id",
			})
			w.WriteHeader(http.StatusFound)

		case r.URL.Path == "/services/v2/eventdefault" && r.Method == "POST" && !loginDone:
			loginDone = true
			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"eventdefault": map[string]interface{}{
						"events": []interface{}{
							map[string]interface{}{
								"pe": []interface{}{
									map[string]interface{}{"id": 965, "ro": 1, "dn": "Teiln: Test User ([BM]Ob956)"},
								},
							},
						},
					},
					"success": true,
				},
			})

		case r.URL.Path == "/services/v2/eventdefault" && r.Method == "POST":
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)

			// Verify request body structure
			if _, ok := body["st"]; !ok {
				t.Error("eventdefault request missing 'st' field")
			}
			if _, ok := body["ca"]; !ok {
				t.Error("eventdefault request missing 'ca' field")
			}
			if _, ok := body["rs"]; !ok {
				t.Error("eventdefault request missing 'rs' field")
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"eventdefault": map[string]interface{}{
						"events": []interface{}{
							map[string]interface{}{
								"st": "2025-06-15T10:00:00.000+02:00",
								"en": "2025-06-15T11:00:00.000+02:00",
								"ca": 1,
								"rs": []interface{}{
									map[string]interface{}{"id": float64(42)},
								},
							},
						},
					},
				},
			})

		case r.URL.Path == "/services/v2/event/type=check" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"success":   true,
					"event_ids": []interface{}{float64(0)},
				},
			})

		case r.URL.Path == "/services/v2/event/type=save" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"success":   true,
					"event_ids": []interface{}{float64(expectedEventID)},
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test@example.com", "password123")

	// Login first
	err := client.Login()
	if err != nil {
		t.Fatalf("Login() returned error: %v", err)
	}

	// Book room
	start := time.Date(2025, 6, 15, 10, 0, 0, 0, time.FixedZone("CET", 2*3600))
	end := time.Date(2025, 6, 15, 12, 0, 0, 0, time.FixedZone("CET", 2*3600))

	result, err := client.BookRoom(42, start, end)
	if err != nil {
		t.Fatalf("BookRoom() returned error: %v", err)
	}

	if result.EventID != expectedEventID {
		t.Errorf("expected EventID %d, got %d", expectedEventID, result.EventID)
	}

	if !result.Success {
		t.Error("expected Success to be true")
	}
}
