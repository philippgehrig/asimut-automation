package asimut

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

const timeFormat = "2006-01-02T15:04:05.000-07:00"

// UserInfo holds user account information from Asimut.
type UserInfo struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Surname        string `json:"surname"`
	Username       string `json:"username"`
	BookingHorizon string `json:"booking_horizon"`
}

// Location represents a bookable location in Asimut.
type Location struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	SecondaryName string `json:"secondary_name"`
	Bookable      bool   `json:"bookable"`
	Type          string `json:"type"`
}

// BookingResult contains the result of a booking operation.
type BookingResult struct {
	EventID int    `json:"event_id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Client communicates with the Asimut scheduling system.
type Client struct {
	mu         sync.Mutex
	baseURL    string
	email      string
	password   string
	httpClient *http.Client
	loggedIn   bool
	userInfo   *UserInfo
}

// NewClient creates a new Asimut client with cookie support.
func NewClient(baseURL, email, password string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		email:    email,
		password: password,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Login authenticates with the Asimut system using form POST.
// It resets the cookie jar to avoid stale session cookies, and retries
// once with a fresh jar if the first attempt fails.
func (c *Client) Login() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.doLogin()
	if err != nil {
		log.Printf("[asimut] first login attempt failed: %v — retrying with fresh session", err)
		// Retry once with a completely fresh cookie jar
		c.resetCookieJar()
		err = c.doLogin()
		if err != nil {
			return fmt.Errorf("login failed after retry: %w", err)
		}
	}

	c.loggedIn = true
	return nil
}

// resetCookieJar replaces the cookie jar with a fresh one to discard stale sessions.
func (c *Client) resetCookieJar() {
	jar, _ := cookiejar.New(nil)
	c.httpClient.Jar = jar
}

// doLogin performs the actual login POST and heartbeat verification.
func (c *Client) doLogin() error {
	// Always start with a fresh cookie jar to avoid stale PHPSESSID
	c.resetCookieJar()

	form := url.Values{}
	form.Set("authenticate-url", "/public/hfm-freiburg.asimut.net")
	form.Set("authenticate-useraccount", c.email)
	form.Set("authenticate-password", c.password)
	form.Set("authenticate-verification", "ok")

	encoded := form.Encode()
	log.Printf("[asimut] login email: %s, password length: %d, password first/last: %c...%c", c.email, len(c.password), c.password[0], c.password[len(c.password)-1])
	log.Printf("[asimut] form body length: %d", len(encoded))

	req, err := http.NewRequest("POST", c.baseURL+"/public/login.php", strings.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", c.baseURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing login request: %w", err)
	}

	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	log.Printf("[asimut] login response: status=%d, cookies=%d, body_length=%d", resp.StatusCode, len(resp.Cookies()), len(respBody))
	if resp.StatusCode != 302 && len(respBody) > 0 {
		snippet := string(respBody)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		log.Printf("[asimut] login response body: %s", snippet)
	}
	for _, cookie := range resp.Cookies() {
		log.Printf("[asimut] set-cookie: %s=%s (domain=%s, path=%s)", cookie.Name, cookie.Value[:min(8, len(cookie.Value))]+"..", cookie.Domain, cookie.Path)
	}

	// Log cookies in jar for the base URL
	if u, err := url.Parse(c.baseURL); err == nil {
		jarCookies := c.httpClient.Jar.Cookies(u)
		log.Printf("[asimut] jar cookies for %s: %d", c.baseURL, len(jarCookies))
		for _, cookie := range jarCookies {
			log.Printf("[asimut] jar cookie: %s=%s..", cookie.Name, cookie.Value[:min(8, len(cookie.Value))])
		}
	}

	// Verify login by checking heartbeat
	loggedIn, err := c.getHeartbeat()
	if err != nil {
		return fmt.Errorf("verifying login: %w", err)
	}
	if !loggedIn {
		return fmt.Errorf("login failed: invalid credentials")
	}

	return nil
}

// LoggedIn returns whether the client is currently authenticated.
func (c *Client) LoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loggedIn
}

// InvalidateSession marks the client as logged out so the next call will re-login.
func (c *Client) InvalidateSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loggedIn = false
}

// GetLocations retrieves all available locations from Asimut.
func (c *Client) GetLocations() ([]Location, error) {
	respBody, err := c.doJSON("GET", "/services/v2/locations", nil)
	if err != nil {
		c.InvalidateSession()
		return nil, fmt.Errorf("getting locations: %w", err)
	}

	response, ok := respBody["response"].(map[string]interface{})
	if !ok {
		c.InvalidateSession()
		return nil, fmt.Errorf("unexpected response format")
	}

	locationsRaw, ok := response["locations"].([]interface{})
	if !ok {
		c.InvalidateSession()
		return nil, fmt.Errorf("unexpected locations format")
	}

	locations := make([]Location, 0, len(locationsRaw))
	for _, lr := range locationsRaw {
		lm, ok := lr.(map[string]interface{})
		if !ok {
			continue
		}
		loc := Location{
			ID:            intFromInterface(lm["id"]),
			Name:          stringFromInterface(lm["name"]),
			SecondaryName: stringFromInterface(lm["secondary_name"]),
			Bookable:      boolFromInterface(lm["bookable"]),
			Type:          stringFromInterface(lm["type"]),
		}
		locations = append(locations, loc)
	}

	return locations, nil
}

// BookRoom books a room for the given time range.
func (c *Client) BookRoom(roomID int, start, end time.Time) (*BookingResult, error) {
	log.Printf("[asimut] book: === booking room %d from %s to %s ===", roomID, start.Format(timeFormat), end.Format(timeFormat))

	// Get event default template
	eventData, err := c.getEventDefault(roomID, start)
	if err != nil {
		log.Printf("[asimut] book: getEventDefault failed: %v", err)
		return nil, fmt.Errorf("getting event default: %w", err)
	}
	log.Printf("[asimut] book: got event default, st=%v en=%v", eventData["st"], eventData["en"])

	// Override end time
	eventData["en"] = end.Format(timeFormat)

	// Ensure st and en use consistent format with milliseconds
	if st, ok := eventData["st"].(string); ok {
		if t, err := time.Parse("2006-01-02T15:04:05-07:00", st); err == nil {
			eventData["st"] = t.Format(timeFormat)
		}
	}

	// Clean nil values from nested structures that cause API 500 errors
	cleanNils(eventData)

	// Wrap event in {"event": ...} envelope as the API expects
	envelope := map[string]interface{}{"event": eventData}

	// Check booking
	log.Printf("[asimut] book: sending check for room %d", roomID)
	checkResp, err := c.doJSONBody("POST", "/services/v2/event/type=check", envelope)
	if err != nil {
		log.Printf("[asimut] book: check request failed: %v", err)
		return nil, fmt.Errorf("checking booking: %w", err)
	}

	if checkResponse, ok := checkResp["response"].(map[string]interface{}); ok {
		if success, _ := checkResponse["success"].(bool); !success {
			msg := "booking check failed"
			if br, ok := checkResponse["bookingrules"].(map[string]interface{}); ok {
				if issues, ok := br["issues"].([]interface{}); ok && len(issues) > 0 {
					if issue, ok := issues[0].(map[string]interface{}); ok {
						if text, ok := issue["text"].(string); ok {
							msg = text
						}
					}
				}
			}
			log.Printf("[asimut] book: check rejected: %s", msg)
			return nil, fmt.Errorf("%s", msg)
		}
		log.Printf("[asimut] book: check passed for room %d", roomID)
	}

	// Save booking
	log.Printf("[asimut] book: sending save for room %d", roomID)
	saveResp, err := c.doJSONBody("POST", "/services/v2/event/type=save", envelope)
	if err != nil {
		log.Printf("[asimut] book: save request failed: %v", err)
		return nil, fmt.Errorf("saving booking: %w", err)
	}
	response, ok := saveResp["response"].(map[string]interface{})
	if !ok {
		log.Printf("[asimut] book: unexpected save response format: %v", saveResp)
		return nil, fmt.Errorf("unexpected save response format")
	}

	success, _ := response["success"].(bool)
	if !success {
		msg := "booking rejected by server"
		if br, ok := response["bookingrules"].(map[string]interface{}); ok {
			if issues, ok := br["issues"].([]interface{}); ok && len(issues) > 0 {
				if issue, ok := issues[0].(map[string]interface{}); ok {
					if text, ok := issue["text"].(string); ok {
						msg = text
					}
				}
			}
		}
		log.Printf("[asimut] book: save rejected: %s", msg)
		return nil, fmt.Errorf("%s", msg)
	}

	eventIDs, ok := response["event_ids"].([]interface{})
	if !ok || len(eventIDs) == 0 {
		log.Printf("[asimut] book: no event ID in save response: %v", response)
		return nil, fmt.Errorf("no event ID in save response")
	}

	eventID := intFromInterface(eventIDs[0])
	log.Printf("[asimut] book: === room %d booked successfully, eventID=%d ===", roomID, eventID)

	return &BookingResult{
		EventID: eventID,
		Success: true,
		Message: "Booking created successfully",
	}, nil
}

// ExtendBooking extends an existing booking to a new end time.
func (c *Client) ExtendBooking(eventID int, newEnd time.Time) (*BookingResult, error) {
	log.Printf("[asimut] extend: === extending event %d to %s ===", eventID, newEnd.Format(timeFormat))

	// Get existing event
	path := fmt.Sprintf("/services/v2/event/event_id=%d", eventID)
	eventResp, err := c.doJSON("GET", path, nil)
	if err != nil {
		log.Printf("[asimut] extend: failed to GET event %d: %v", eventID, err)
		return nil, fmt.Errorf("getting event: %w", err)
	}

	log.Printf("[asimut] extend: GET event %d response keys: %v", eventID, keys(eventResp))

	response, ok := eventResp["response"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected event response format: %v", eventResp)
	}

	event, ok := response["event"].(map[string]interface{})
	if !ok {
		log.Printf("[asimut] extend: response keys: %v", keys(response))
		return nil, fmt.Errorf("unexpected event format, available keys: %v", keys(response))
	}

	log.Printf("[asimut] extend: current event st=%v en=%v, updating en to %s", event["st"], event["en"], newEnd.Format(timeFormat))

	// Update end time
	event["en"] = newEnd.Format(timeFormat)

	// Clean nil values that cause API 500 errors
	cleanNils(event)

	// Wrap in {"event": ...} envelope
	envelope := map[string]interface{}{"event": event}

	// Check extension
	checkPath := fmt.Sprintf("/services/v2/event/event_id=%d;type=check", eventID)
	log.Printf("[asimut] extend: sending PATCH check for event %d", eventID)
	checkResp, err := c.doJSONBody("PATCH", checkPath, envelope)
	if err != nil {
		log.Printf("[asimut] extend: PATCH check request failed: %v", err)
		return nil, fmt.Errorf("checking extension: %w", err)
	}

	if checkResponse, ok := checkResp["response"].(map[string]interface{}); ok {
		if success, _ := checkResponse["success"].(bool); !success {
			msg := "extension check failed"
			if br, ok := checkResponse["bookingrules"].(map[string]interface{}); ok {
				if issues, ok := br["issues"].([]interface{}); ok && len(issues) > 0 {
					if issue, ok := issues[0].(map[string]interface{}); ok {
						if text, ok := issue["text"].(string); ok {
							msg = text
						}
					}
				}
			}
			log.Printf("[asimut] extend: check rejected: %s (full response: %v)", msg, checkResp)
			return nil, fmt.Errorf("%s", msg)
		}
		log.Printf("[asimut] extend: check passed for event %d", eventID)
	} else {
		log.Printf("[asimut] extend: check response has unexpected structure: %v", checkResp)
	}

	// Save extension
	savePath := fmt.Sprintf("/services/v2/event/event_id=%d;type=save", eventID)
	log.Printf("[asimut] extend: sending PATCH save for event %d", eventID)
	saveResp, err := c.doJSONBody("PATCH", savePath, envelope)
	if err != nil {
		log.Printf("[asimut] extend: PATCH save request failed: %v", err)
		return nil, fmt.Errorf("saving extension: %w", err)
	}

	if saveResponse, ok := saveResp["response"].(map[string]interface{}); ok {
		if success, _ := saveResponse["success"].(bool); !success {
			msg := "extension save rejected"
			if br, ok := saveResponse["bookingrules"].(map[string]interface{}); ok {
				if issues, ok := br["issues"].([]interface{}); ok && len(issues) > 0 {
					if issue, ok := issues[0].(map[string]interface{}); ok {
						if text, ok := issue["text"].(string); ok {
							msg = text
						}
					}
				}
			}
			log.Printf("[asimut] extend: save rejected: %s (full response: %v)", msg, saveResp)
			return nil, fmt.Errorf("%s", msg)
		}
		log.Printf("[asimut] extend: save succeeded for event %d", eventID)
	} else {
		log.Printf("[asimut] extend: save response has unexpected structure: %v", saveResp)
	}

	log.Printf("[asimut] extend: === event %d successfully extended to %s ===", eventID, newEnd.Format(timeFormat))
	return &BookingResult{
		EventID: eventID,
		Success: true,
		Message: "Booking extended successfully",
	}, nil
}

// GetEvent fetches a single event by ID and returns its info.
func (c *Client) GetEvent(eventID int) (*AsimutEventInfo, error) {
	path := fmt.Sprintf("/services/v2/event/event_id=%d", eventID)
	respBody, err := c.doJSON("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("getting event %d: %w", eventID, err)
	}

	response, ok := respBody["response"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected event response format")
	}

	event, ok := response["event"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("event %d not found", eventID)
	}

	roomName := ""
	if rs, ok := event["rs"].([]interface{}); ok && len(rs) > 0 {
		if rm, ok := rs[0].(map[string]interface{}); ok {
			roomName = stringFromInterface(rm["dn"])
			if name := stringFromInterface(rm["name"]); name != "" {
				roomName = name
			}
			if idx := strings.Index(roomName, " ("); idx > 0 {
				roomName = roomName[:idx]
			}
		}
	}

	return &AsimutEventInfo{
		ID:        intFromInterface(event["id"]),
		Title:     stringFromInterface(event["ar"]),
		RoomName:  roomName,
		StartTime: stringFromInterface(event["st"]),
		EndTime:   stringFromInterface(event["en"]),
	}, nil
}

// AsimutEventInfo represents an event fetched from the Asimut calendar.
type AsimutEventInfo struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	RoomName  string `json:"room_name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// GetMyEvents fetches the logged-in user's events between from and to.
func (c *Client) GetMyEvents(from, to time.Time) ([]AsimutEventInfo, error) {
	fromStr := from.Format(timeFormat)

	path := fmt.Sprintf("/services/v2/search/type=events;load_from=%s;direction=forward;is_participating=true;may_signup=false;limit=100", fromStr)

	body := map[string]interface{}{
		"search": "Einzelüben",
	}

	respBody, err := c.doJSONBody("POST", path, body)
	if err != nil {
		return nil, fmt.Errorf("searching events: %w", err)
	}

	log.Printf("[asimut] events search response keys: %v", keys(respBody))

	response, ok := respBody["response"].(map[string]interface{})
	if !ok {
		respJSON, _ := json.Marshal(respBody)
		return nil, fmt.Errorf("unexpected events response format: %s", string(respJSON[:min(500, len(respJSON))]))
	}

	eventsMap, ok := response["events"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no events map in response (keys: %v)", keys(response))
	}

	var events []AsimutEventInfo
	for _, dayGroups := range eventsMap {
		groups, ok := dayGroups.([]interface{})
		if !ok {
			continue
		}
		for _, g := range groups {
			group, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			eventsRaw, ok := group["events"].([]interface{})
			if !ok {
				continue
			}
			for _, er := range eventsRaw {
				em, ok := er.(map[string]interface{})
				if !ok {
					continue
				}

				roomName := ""
				if rs, ok := em["rs"].([]interface{}); ok && len(rs) > 0 {
					if rm, ok := rs[0].(map[string]interface{}); ok {
						roomName = stringFromInterface(rm["dn"])
						if name := stringFromInterface(rm["name"]); name != "" {
							roomName = name
						}
						if idx := strings.Index(roomName, " ("); idx > 0 {
							roomName = roomName[:idx]
						}
					}
				}

				events = append(events, AsimutEventInfo{
					ID:        intFromInterface(em["id"]),
					Title:     stringFromInterface(em["ar"]),
					RoomName:  roomName,
					StartTime: stringFromInterface(em["st"]),
					EndTime:   stringFromInterface(em["en"]),
				})
			}
		}
	}

	log.Printf("[asimut] parsed %d events from search", len(events))
	return events, nil
}

// getEventDefault retrieves the default event template for a room.
func (c *Client) getEventDefault(roomID int, start time.Time) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"st": start.Format(timeFormat),
		"ca": 1,
		"rs": []map[string]interface{}{
			{"id": roomID},
		},
	}

	respBody, err := c.doJSONBody("POST", "/services/v2/eventdefault", body)
	if err != nil {
		return nil, fmt.Errorf("getting event default: %w", err)
	}

	response, ok := respBody["response"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected eventdefault response format")
	}

	eventDefault, ok := response["eventdefault"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected eventdefault format")
	}

	events, ok := eventDefault["events"].([]interface{})
	if !ok || len(events) == 0 {
		return nil, fmt.Errorf("no events in eventdefault response")
	}

	event, ok := events[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected event format in eventdefault")
	}

	return event, nil
}

// getHeartbeat checks if the session is still authenticated.
func (c *Client) getHeartbeat() (bool, error) {
	respBody, err := c.doJSON("GET", "/services/v2/heartbeat/me", nil)
	if err != nil {
		log.Printf("[asimut] heartbeat error: %v", err)
		return false, fmt.Errorf("getting heartbeat: %w", err)
	}

	log.Printf("[asimut] heartbeat raw response: %v", respBody)

	response, ok := respBody["response"].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("unexpected heartbeat response format")
	}

	heartbeat, ok := response["heartbeat"].(map[string]interface{})
	if !ok {
		log.Printf("[asimut] heartbeat missing 'heartbeat' key, response keys: %v", response)
		return false, fmt.Errorf("unexpected heartbeat format")
	}

	loggedIn, ok := heartbeat["loggedin"].(bool)
	if !ok {
		log.Printf("[asimut] heartbeat 'loggedin' not a bool: %v (type %T)", heartbeat["loggedin"], heartbeat["loggedin"])
		return false, nil
	}

	if loggedIn {
		if me, ok := heartbeat["me"].(map[string]interface{}); ok {
			c.userInfo = &UserInfo{
				ID:             intFromInterface(me["id"]),
				Name:           stringFromInterface(me["name"]),
				Surname:        stringFromInterface(me["surname"]),
				Username:       stringFromInterface(me["username"]),
				BookingHorizon: stringFromInterface(me["booking_horizon"]),
			}
			log.Printf("[asimut] user info: id=%d, name=%s %s", c.userInfo.ID, c.userInfo.Name, c.userInfo.Surname)
		}
	}

	log.Printf("[asimut] heartbeat loggedin=%v", loggedIn)
	return loggedIn, nil
}

// doJSON makes an HTTP request and returns the parsed JSON response.
func (c *Client) doJSON(method, path string, body io.Reader) (map[string]interface{}, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", c.baseURL)
	if method == "POST" || method == "PATCH" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Log cookies being sent
	if u, err := url.Parse(c.baseURL + path); err == nil {
		cookies := c.httpClient.Jar.Cookies(u)
		log.Printf("[asimut] %s %s — sending %d cookies", method, path, len(cookies))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[asimut] %s %s — status %d", method, path, resp.StatusCode)

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if len(respBytes) == 0 {
		return map[string]interface{}{}, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON response (status %d, body: %s): %w", resp.StatusCode, string(respBytes[:min(200, len(respBytes))]), err)
	}

	return result, nil
}

// doJSONBody makes an HTTP request with a JSON body and returns the parsed response.
func (c *Client) doJSONBody(method, path string, body interface{}) (map[string]interface{}, error) {
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request body: %w", err)
	}

	return c.doJSON(method, path, bytes.NewReader(jsonBytes))
}

// Helper functions for type conversion from interface{}.

func intFromInterface(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case json.Number:
		n, _ := val.Int64()
		return int(n)
	default:
		return 0
	}
}

func stringFromInterface(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolFromInterface(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func keys(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// cleanNils recursively removes nil values from maps and slices.
func cleanNils(v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if v == nil {
				delete(val, k)
			} else {
				cleanNils(v)
			}
		}
	case []interface{}:
		for _, item := range val {
			cleanNils(item)
		}
	}
}
