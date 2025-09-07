package cmd

import (
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
    Use:   "capes [username]",
    Short: "display minecraft capes in your terminal",
    Long:  `minimal cli to fetch minecraft capes and skins from capes.me with caching`,
    Args:  cobra.ExactArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        username := args[0]
        fetchUserCapes(username)
    },
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Println(err)
        os.Exit(1)
    }
}

// Cape structure for /api/capes
type Cape struct {
    URL   string `json:"url"`
    Type  string `json:"type"`
    Title string `json:"title"`
}

// User structure for /api/user/:username
type User struct {
    Username string `json:"username"`
    UUID     string `json:"uuid"`
    Capes    []struct {
        Type    string `json:"type"`
        Removed bool   `json:"removed"`
    } `json:"capes"`
}

const cacheFile = "capes.json"
const cacheTTL = 24 * time.Hour // refresh once per day

func fetchUserCapes(username string) {
    client := &http.Client{}

    // 1️⃣ fetch or load cape cache
    allCapes := loadCapeCache(client)

    // 2️⃣ fetch user data
    userURL := fmt.Sprintf("https://capes.me/api/user/%s", username)
    userReq, _ := http.NewRequest("GET", userURL, nil)
    userReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36")

    userResp, err := client.Do(userReq)
    if err != nil {
        log.Fatalf("failed to fetch user data: %v", err)
    }
    defer userResp.Body.Close()

    if userResp.StatusCode != 200 {
        fmt.Printf("error: status code %d\n", userResp.StatusCode)
        return
    }

    body, _ := io.ReadAll(userResp.Body)
    var user User
    if err := json.Unmarshal(body, &user); err != nil {
        log.Fatalf("failed to parse user json: %v", err)
    }

    fmt.Printf("username: %s\nuuid: %s\n", user.Username, user.UUID)

    // 3️⃣ match user capes to URLs
    userCapeURLs := []string{}
    for _, userCape := range user.Capes {
        for _, cape := range allCapes {
            if cape.Type == userCape.Type {
                userCapeURLs = append(userCapeURLs, cape.URL)
            }
        }
    }

    // 4️⃣ output
    fmt.Println("cape URLs:")
    for _, url := range userCapeURLs {
        fmt.Println(url)
    }
}

// loadCapeCache loads the cape cache or fetches it if missing/expired
func loadCapeCache(client *http.Client) []Cape {
    var cached []Cape

    info, err := os.Stat(cacheFile)
    if err == nil && time.Since(info.ModTime()) < cacheTTL {
        // cache exists and is fresh
        f, err := os.Open(cacheFile)
        if err == nil {
            defer f.Close()
            if err := json.NewDecoder(f).Decode(&cached); err == nil {
                return cached
            }
        }
    }

    // fetch from API
    capesURL := "https://capes.me/api/capes"
    req, _ := http.NewRequest("GET", capesURL, nil)
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36")

    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("failed to fetch capes: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        log.Fatalf("error fetching capes: status code %d", resp.StatusCode)
    }

    body, _ := io.ReadAll(resp.Body)
    if err := json.Unmarshal(body, &cached); err != nil {
        log.Fatalf("failed to parse capes json: %v", err)
    }

    // save cache
    f, err := os.Create(cacheFile)
    if err == nil {
        defer f.Close()
        _ = json.NewEncoder(f).Encode(cached)
    }

    return cached
}
