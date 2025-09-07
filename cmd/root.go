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
    resp, err := http.Get(url)
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
