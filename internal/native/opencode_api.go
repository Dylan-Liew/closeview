package native

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// V2 deletion must update the service's event and projection state together.
func deleteOpenCodeV2(ctx context.Context, id string) error {
	endpoint := os.Getenv("CLOSEVIEW_OPENCODE_URL")
	password := os.Getenv("CLOSEVIEW_OPENCODE_PASSWORD")
	if file := os.Getenv("CLOSEVIEW_OPENCODE_PASSWORD_FILE"); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var service struct {
			URL      string `json:"url"`
			Password string `json:"password"`
		}
		if json.Unmarshal(data, &service) == nil {
			if endpoint == "" {
				endpoint = service.URL
			}
			if password == "" {
				password = service.Password
			}
		} else if password == "" {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "OPENCODE_SERVER_PASSWORD=") {
					password = strings.Trim(strings.TrimPrefix(line, "OPENCODE_SERVER_PASSWORD="), "\"'\r ")
				}
			}
		}
	}
	if endpoint == "" {
		return fmt.Errorf("set CLOSEVIEW_OPENCODE_URL or provide a V2 service file to delete OpenCode sessions through its API")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(endpoint, "/")+"/api/session/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if password != "" {
		request.SetBasicAuth("opencode", password)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("OpenCode V2 deletion returned HTTP %d", response.StatusCode)
	}
	return nil
}
