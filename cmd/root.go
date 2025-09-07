package cmd

import (
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"

    "github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
    Use:   "capes [username]",
    Short: "display minecraft capes in your terminal",
    Long:  `minimal cli to fetch minecraft capes and skins from capes.me`,
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

func fetchUserCapes(username string) {
    url := fmt.Sprintf("https://capes.me/api/user/%s", username)

    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        log.Fatalf("failed to create request: %v", err)
    }

    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("failed to fetch data: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        fmt.Printf("error: status code %d\n", resp.StatusCode)
        return
    }

    body, _ := io.ReadAll(resp.Body)

    var data map[string]interface{}
    if err := json.Unmarshal(body, &data); err != nil {
        log.Fatalf("failed to parse json: %v", err)
    }

    fmt.Printf("username: %s\n", data["username"])
    fmt.Printf("uuid: %s\n", data["uuid"])
    fmt.Printf("capes: %v\n", data["capes"])
}