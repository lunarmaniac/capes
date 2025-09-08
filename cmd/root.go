package cmd

import (
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "time"
    "image"
    "image/draw"
    "image/png"
    "github.com/spf13/cobra"
    "golang.org/x/image/font"
    "golang.org/x/image/font/basicfont"
    "golang.org/x/image/math/fixed"
)

// config structure
type Config struct {
    DefaultUsername string `json:"default_username"`
    
    Display struct {
        HeadSize        int `json:"head_size"`         // size for head display (terminal rows)
        LayoutHeight    int `json:"layout_height"`     // height for complete layout display
        UpscaleFactor   int `json:"upscale_factor"`    // image upscaling factor (2x, 4x, etc)
        ShowHeadOnly    bool `json:"show_head_only"`   // show only head when no capes found
        ImageBackend string `json:"image_backend"` // e.g., "kitty", "chafa"
    } `json:"display"`
    
    Layout struct {
        Spacing           int `json:"spacing"`            // space between major elements
        CapeSpacing       int `json:"cape_spacing"`       // space between individual capes
        CapesPerRow       int `json:"capes_per_row"`      // max capes per row (0 = auto)
        UsernameRowHeight int `json:"username_row_height"` // height allocated for username text
        CapeStartOffset   int `json:"cape_start_offset"`   // vertical offset for cape positioning
        AvailableCapeWidth int `json:"available_cape_width"` // available width for cape area when auto calculating
    } `json:"layout"`
    
    Cache struct {
        CacheTTLHours int    `json:"cache_ttl_hours"`    // cache TTL in hours
        CacheDir      string `json:"cache_dir"`          // custom cache directory
    } `json:"cache"`
    
    Network struct {
        TimeoutSeconds int    `json:"timeout_seconds"`    // HTTP timeout
        UserAgent      string `json:"user_agent"`         // custom user agent
    } `json:"network"`
}

// global config variable
var appConfig Config

var rootCmd = &cobra.Command{
    Use:   "capes [username]",
    Short: "display minecraft capes and head in terminal",
    Long:  "minimal CLI to fetch minecraft capes and player head from capes.me and render in Kitty terminal",
    Args:  cobra.MaximumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        var username string
        
        if len(args) > 0 {
            username = args[0]
        } else if appConfig.DefaultUsername != "" {
            username = appConfig.DefaultUsername
        } else {
            fmt.Println("Error: no username provided and no default username configured")
            fmt.Println("Usage: capes [username]")
            fmt.Printf("Or set \"default_username\" in: %s\n", getConfigPath())
            os.Exit(1)
        }
        
        displayPlayer(username)
    },
}

func init() {
    loadConfig()
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Println(err)
        os.Exit(1)
    }
}

// default configuration values
func getDefaultConfig() Config {
    config := Config{}
    
    config.DefaultUsername = ""
    
    // display settings
    config.Display.HeadSize = 8
    config.Display.LayoutHeight = 48
    config.Display.UpscaleFactor = 4
    config.Display.ShowHeadOnly = true
    config.Display.ImageBackend = "kitty"
    
    // layout settings
    config.Layout.Spacing = 5
    config.Layout.CapeSpacing = 2
    config.Layout.CapesPerRow = 0 // auto calculate
    config.Layout.UsernameRowHeight = 14
    config.Layout.CapeStartOffset = 2
    config.Layout.AvailableCapeWidth = 200
    
    // cache settings
    config.Cache.CacheTTLHours = 24
    config.Cache.CacheDir = "cache"
    
    // network settings
    config.Network.TimeoutSeconds = 15
    config.Network.UserAgent = "Mozilla/5.0 (capes-cli)"
    
    return config
}

func getConfigDir() string {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        log.Fatalf("failed to get home directory: %v", err)
    }
    
    // follow XDG base directory specification
    configHome := os.Getenv("XDG_CONFIG_HOME")
    if configHome == "" {
        configHome = filepath.Join(homeDir, ".config")
    }
    
    return filepath.Join(configHome, "capes")
}

func getConfigPath() string {
    return filepath.Join(getConfigDir(), "config.json")
}

func loadConfig() {
    appConfig = getDefaultConfig()
    configPath := getConfigPath()
    
    // check if config file exists
    if _, err := os.Stat(configPath); os.IsNotExist(err) {
        // create config directory and default config file
        configDir := getConfigDir()
        if err := os.MkdirAll(configDir, 0755); err != nil {
            log.Printf("warning: failed to create config directory: %v", err)
            return
        }
        
        // write default config
        data, err := json.MarshalIndent(appConfig, "", "  ")
        if err != nil {
            log.Printf("warning: failed to marshal default config: %v", err)
            return
        }
        
        if err := os.WriteFile(configPath, data, 0644); err != nil {
            log.Printf("warning: failed to write config file: %v", err)
            return
        }
        
        fmt.Printf("created config file: %s\n", configPath)
        fmt.Println("you can edit this file to customize your settings")
        return
    }
    
    // load existing config
    data, err := os.ReadFile(configPath)
    if err != nil {
        log.Printf("warning: failed to read config file: %v", err)
        return
    }
    
    if err := json.Unmarshal(data, &appConfig); err != nil {
        log.Printf("warning: failed to parse config file, using defaults: %v", err)
        appConfig = getDefaultConfig()
        return
    }
}

type Cape struct {
    URL   string `json:"url"`
    Type  string `json:"type"`
    Title string `json:"title"`
}

type User struct {
    Username string `json:"username"`
    UUID     string `json:"uuid"`
    Capes    []struct {
        Type    string `json:"type"`
        Removed bool   `json:"removed"`
    } `json:"capes"`
}

func displayPlayer(username string) {
    cacheDir := appConfig.Cache.CacheDir
    capeDir := filepath.Join(cacheDir, "capes")
    headDir := filepath.Join(cacheDir, "heads")
    
    os.MkdirAll(capeDir, 0755)
    os.MkdirAll(headDir, 0755)

    client := &http.Client{
        Timeout: time.Duration(appConfig.Network.TimeoutSeconds) * time.Second,
    }

    allCapes := loadCapeCache(client)
    user := fetchUser(client, username)

    // doownload player head (might change to headshot in future from : https://capes.me/images/skins/bust/b05881186e75410db2db4d3066b223f7
    headPath := filepath.Join(headDir, user.UUID+".png")
    downloadIfNeeded(client, "https://crafatar.com/avatars/"+user.UUID+"?size=32&overlay", 
                     headPath, time.Duration(appConfig.Cache.CacheTTLHours)*time.Hour)

    // collect valid capes
    var validCapes []Cape
    
    for _, uc := range user.Capes {
        if uc.Removed {
            continue
        }
        for _, cape := range allCapes {
            if cape.Type == uc.Type {
                validCapes = append(validCapes, cape)
                break
            }
        }
    }

    if len(validCapes) == 0 {
        if appConfig.Display.ShowHeadOnly {
            renderImageToTerminal(headPath, appConfig.Display.HeadSize)
        } else {
            fmt.Printf("no capes found for user: %s\n", username)
        }
        return
    }

    // download and crop all capes
    var croppedPaths []string
    for _, cape := range validCapes {
        capePath := filepath.Join(capeDir, cape.Type+".png")
        downloadIfNeeded(client, cape.URL, capePath, 
                        time.Duration(appConfig.Cache.CacheTTLHours)*time.Hour)

        tempCroppedPath := filepath.Join(capeDir, cape.Type+"_cropped.png")
        if err := cropCape(capePath, tempCroppedPath); err != nil {
            log.Printf("failed to crop cape %s: %v", cape.Type, err)
            continue
        }
        croppedPaths = append(croppedPaths, tempCroppedPath)
    }

    if len(croppedPaths) == 0 {
        if appConfig.Display.ShowHeadOnly {
            renderImageToTerminal(headPath, appConfig.Display.HeadSize)
        }
        return
    }

    // create player layout image to be displayed (head + username + capes)
    layoutPath := filepath.Join(cacheDir, "player_layout.png")
    if err := createPlayerLayout(headPath, croppedPaths, user.Username, user.UUID, layoutPath); err != nil {
        log.Printf("failed to create player layout: %v", err)
        return
    }

    // display the complete layout
    renderImageToTerminal(layoutPath, appConfig.Display.LayoutHeight)
}

func createPlayerLayout(headPath string, capePaths []string, username string, uuid string, outputPath string) error {
    if len(capePaths) == 0 {
        return fmt.Errorf("no capes to display")
    }

    // load head image
    headImg, err := loadImage(headPath)
    if err != nil {
        return err
    }
    
    // load first cape to get cape dimensions
    firstCape, err := loadImage(capePaths[0])
    if err != nil {
        return err
    }
    
    capeWidth := firstCape.Bounds().Dx()
    capeHeight := firstCape.Bounds().Dy()
    headWidth := headImg.Bounds().Dx()
    headHeight := headImg.Bounds().Dy()
    
    // use config values for layout
    spacing := appConfig.Layout.Spacing
    capeSpacing := appConfig.Layout.CapeSpacing
    usernameRowHeight := appConfig.Layout.UsernameRowHeight
    capeStartOffset := appConfig.Layout.CapeStartOffset
    
    // calculate capes per row
    capesPerRow := appConfig.Layout.CapesPerRow
    if capesPerRow <= 0 {
        // auto calculate based on available width
        availableWidthForCapes := appConfig.Layout.AvailableCapeWidth
        capesPerRow = availableWidthForCapes / (capeWidth + capeSpacing)
        if capesPerRow < 1 {
            capesPerRow = 1
        }
    }
    if capesPerRow > len(capePaths) {
        capesPerRow = len(capePaths)
    }
    
    // calculate cape area dimensions
    capeRows := (len(capePaths) + capesPerRow - 1) / capesPerRow
    capeAreaWidth := capesPerRow * capeWidth + (capesPerRow - 1) * capeSpacing
    capeAreaHeight := capeRows * capeHeight + (capeRows - 1) * capeSpacing
    
    // estimate username width (approximately 7 pixels per character with basicfont.Face7x13)
    usernameWidth := len(username) * 7
    
    // calculate minimum width needed for username display
    minWidthForUsername := headWidth + spacing + usernameWidth
    
    // total composite dimensions.. ensure we have enough width for both capes and username
    totalWidth := headWidth + spacing + capeAreaWidth
    if totalWidth < minWidthForUsername {
        totalWidth = minWidthForUsername
    }
    
    totalHeight := headHeight
    capeStartFromTop := usernameRowHeight + capeStartOffset
    
    if capeStartFromTop + capeAreaHeight > totalHeight {
        totalHeight = capeStartFromTop + capeAreaHeight
    }
    
    composite := image.NewRGBA(image.Rect(0, 0, totalWidth, totalHeight))
    
    // draw head image at top left
    draw.Draw(composite, image.Rect(0, 0, headWidth, headHeight), headImg, image.Point{}, draw.Src)
    
    // draw username to the right of head, at the top (adjust position based on config)
    textY := usernameRowHeight - 4 // adjust text position within the row height
    drawText(composite, headWidth + spacing, textY, username)
    
    // cape area starts to the right of head, below username
    capeStartX := headWidth + spacing
    capeStartY := capeStartFromTop
    
    // draw capes in grid to the right of head
    for i, capePath := range capePaths {
        img, err := loadImage(capePath)
        if err != nil {
            log.Printf("failed to load cape %s: %v", capePath, err)
            continue
        }
        
        row := i / capesPerRow
        col := i % capesPerRow
        
        capeX := capeStartX + col * (capeWidth + capeSpacing)
        capeY := capeStartY + row * (capeHeight + capeSpacing)
        
        draw.Draw(composite, image.Rect(capeX, capeY, capeX+capeWidth, capeY+capeHeight), img, image.Point{}, draw.Src)
    }
    
    // save composite
    out, err := os.Create(outputPath)
    if err != nil {
        return err
    }
    defer out.Close()
    
    return png.Encode(out, composite)
}

// simple text drawing function using basic font (might change to minecraft font in future xd)
func drawText(img *image.RGBA, x, y int, text string) {
    col := image.White
    point := fixed.Point26_6{fixed.Int26_6(x * 64), fixed.Int26_6(y * 64)}
    
    d := &font.Drawer{
        Dst:  img,
        Src:  image.NewUniform(col),
        Face: basicfont.Face7x13,
        Dot:  point,
    }
    d.DrawString(text)
}

func loadImage(path string) (image.Image, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    
    img, err := png.Decode(f)
    if err != nil {
        return nil, err
    }
    
    return img, nil
}

func cropCape(inputPath, outputPath string) error {
    f, err := os.Open(inputPath)
    if err != nil {
        return err
    }
    defer f.Close()

    img, err := png.Decode(f)
    if err != nil {
        return err
    }

    bounds := img.Bounds()
    rect := image.Rect(1, 1, 11, 17) // crop 10x16 area

    if rect.Max.X > bounds.Max.X || rect.Max.Y > bounds.Max.Y {
        rect = bounds
    }

    cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
    for y := rect.Min.Y; y < rect.Max.Y; y++ {
        for x := rect.Min.X; x < rect.Max.X; x++ {
            cropped.Set(x-rect.Min.X, y-rect.Min.Y, img.At(x, y))
        }
    }

    out, err := os.Create(outputPath)
    if err != nil {
        return err
    }
    defer out.Close()

    return png.Encode(out, cropped)
}

// renderImageToTerminal upscales the image using config values
func renderImageToTerminal(path string, targetHeight int) {
    // create a temporary file for the upscaled image
    tmpUpscaled := filepath.Join(os.TempDir(), "tmp_upscaled.png")

    // upscale using config factor with nearest neighbor
    upscalePercent := fmt.Sprintf("%d%%", appConfig.Display.UpscaleFactor*100)
    cmdUpscale := exec.Command("magick", path, "-scale", upscalePercent, tmpUpscaled)
    cmdUpscale.Stdout = nil
    cmdUpscale.Stderr = nil
    if err := cmdUpscale.Run(); err != nil {
        log.Printf("failed to upscale %s: %v", path, err)
        return
    }

    // determine backend command
    var cmd *exec.Cmd
    switch appConfig.Display.ImageBackend {
    case "kitty":
        cmd = exec.Command("kitty", "+kitten", "icat", "--align", "left", tmpUpscaled)

    case "chafa":
        cmd = exec.Command("chafa", tmpUpscaled, "--fill=block", "--symbols=block")

    default:
        log.Printf("unknown image backend '%s', defaulting to Kitty", appConfig.Display.ImageBackend)
        cmd = exec.Command("kitty", "+kitten", "icat", "--align", "left", tmpUpscaled)
    }

    // redirect stdout/stderr trashh
    cmd.Stdout = os.Stdout
    devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
    if err == nil {
        cmd.Stderr = devNull
        defer devNull.Close()
    }

    if err := cmd.Run(); err != nil {
        log.Printf("failed to display %s using %s: %v", tmpUpscaled, appConfig.Display.ImageBackend, err)
        return
    }

    // remove temporary upscaled file
    _ = os.Remove(tmpUpscaled)
}

func fetchUser(client *http.Client, username string) User {
    url := fmt.Sprintf("https://capes.me/api/user/%s", username)
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("User-Agent", appConfig.Network.UserAgent)

    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("failed to fetch user: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        log.Fatalf("error: status code %d", resp.StatusCode)
    }

    body, _ := io.ReadAll(resp.Body)
    var user User
    if err := json.Unmarshal(body, &user); err != nil {
        log.Fatalf("failed to parse user JSON: %v", err)
    }

    return user
}

func loadCapeCache(client *http.Client) []Cape {
    cacheFile := filepath.Join(appConfig.Cache.CacheDir, "capes", "capes.json")
    var cached []Cape

    cacheTTL := time.Duration(appConfig.Cache.CacheTTLHours) * time.Hour
    info, err := os.Stat(cacheFile)
    if err == nil && time.Since(info.ModTime()) < cacheTTL {
        f, err := os.Open(cacheFile)
        if err == nil {
            defer f.Close()
            _ = json.NewDecoder(f).Decode(&cached)
            return cached
        }
    }

    req, _ := http.NewRequest("GET", "https://capes.me/api/capes", nil)
    req.Header.Set("User-Agent", appConfig.Network.UserAgent)
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("failed to fetch capes: %v", err)
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    if err := json.Unmarshal(body, &cached); err != nil {
        log.Fatalf("failed to parse capes JSON: %v", err)
    }

    // save cache
    f, err := os.Create(cacheFile)
    if err == nil {
        defer f.Close()
        _ = json.NewEncoder(f).Encode(cached)
    }

    return cached
}

// downloadIfNeeded downloads a file if missing or expired
func downloadIfNeeded(client *http.Client, url, path string, ttl time.Duration) {
    info, err := os.Stat(path)
    if err == nil && time.Since(info.ModTime()) < ttl {
        return // cached and fresh
    }

    resp, err := client.Get(url)
    if err != nil {
        log.Printf("failed to download %s: %v", url, err)
        return
    }
    defer resp.Body.Close()

    f, err := os.Create(path)
    if err != nil {
        log.Printf("failed to save %s: %v", path, err)
        return
    }
    defer f.Close()

    _, _ = io.Copy(f, resp.Body)
}