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

var rootCmd = &cobra.Command{
    Use:   "capes [username]",
    Short: "display minecraft capes and head in terminal",
    Long:  "minimal CLI to fetch minecraft capes and player head from capes.me and render in Kitty terminal",
    Args:  cobra.ExactArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        username := args[0]
        displayPlayer(username)
    },
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Println(err)
        os.Exit(1)
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

const cacheDir = "cache"
const capeDir = cacheDir + "/capes"
const headDir = cacheDir + "/heads"
const capeCacheTTL = 24 * time.Hour
const headCacheTTL = 24 * time.Hour

func displayPlayer(username string) {
    os.MkdirAll(capeDir, 0755)
    os.MkdirAll(headDir, 0755)

    client := &http.Client{Timeout: 15 * time.Second}

    allCapes := loadCapeCache(client)
    user := fetchUser(client, username)

    // doownload player head (might change to headshot in future from : https://capes.me/images/skins/bust/b05881186e75410db2db4d3066b223f7
    headPath := filepath.Join(headDir, user.UUID+".png")
    downloadIfNeeded(client, "https://crafatar.com/avatars/"+user.UUID+"?size=32&overlay", headPath, headCacheTTL)

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
        // still show head even if no capes (might be an issue if capes.me replies 404 like it has in past)
        renderImageKitty(headPath, 8) // head size in term rows i think
        return
    }

    // download and crop all capes
    var croppedPaths []string
    for _, cape := range validCapes {
        capePath := filepath.Join(capeDir, cape.Type+".png")
        downloadIfNeeded(client, cape.URL, capePath, capeCacheTTL)

        tempCroppedPath := filepath.Join(capeDir, cape.Type+"_cropped.png")
        if err := cropCape(capePath, tempCroppedPath); err != nil {
            log.Printf("failed to crop cape %s: %v", cape.Type, err)
            continue
        }
        croppedPaths = append(croppedPaths, tempCroppedPath)
    }

    if len(croppedPaths) == 0 {
        renderImageKitty(headPath, 8)
        return
    }

    // create player layout image to be displayed (head + username + capes)
    layoutPath := filepath.Join(cacheDir, "player_layout.png")
    if err := createPlayerLayout(headPath, croppedPaths, user.Username, user.UUID, layoutPath); err != nil {
        log.Printf("failed to create player layout: %v", err)
        return
    }

    // display the complete layout
    renderImageKitty(layoutPath, 48)
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
    
    // text dimensions
    textHeight := 10
    
    // calculate layout dimensions
    spacing := 5 // space between elements
    capeSpacing := 2 // space between capes
    
    // calculate how many capes fit horizontally to the right of head
    availableWidthForCapes := 200 // reasonable width for cape area
    capesPerRow := availableWidthForCapes / (capeWidth + capeSpacing)
    if capesPerRow < 1 {
        capesPerRow = 1
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
    usernameRowHeight := textHeight + 4 // changing this effects vertical position of the capes, useful for aligning with the buttom of the head lol
    capeStartFromTop := usernameRowHeight + 2 // capes start just below username
    
    if capeStartFromTop + capeAreaHeight > totalHeight {
        totalHeight = capeStartFromTop + capeAreaHeight
    }
    
    composite := image.NewRGBA(image.Rect(0, 0, totalWidth, totalHeight))
    
    // draw head image at top left
    draw.Draw(composite, image.Rect(0, 0, headWidth, headHeight), headImg, image.Point{}, draw.Src)
    
    // draw username to the right of head, at the top
    drawText(composite, headWidth + spacing, textHeight, username)
    
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

// renderImageKitty upscales the image by 4x cuz it looks better
func renderImageKitty(path string, targetHeight int) {
    // create a temporary file for the upscaled image
    tmpUpscaled := filepath.Join(os.TempDir(), "tmp_upscaled.png")

    // upscale 4x with nearest neighbor
    cmdUpscale := exec.Command("magick", path, "-scale", "400%", tmpUpscaled)
    // suppress both stdout and stderr to avoid imagemagick warnings because it complains even tho it works..?
    cmdUpscale.Stdout = nil
    cmdUpscale.Stderr = nil
    if err := cmdUpscale.Run(); err != nil {
        log.Printf("failed to upscale %s: %v", path, err)
        return
    }

    // display in Kitty with left alignment and stderr suppressed
    cmdKitty := exec.Command("kitty", "+kitten", "icat", "--align", "left", tmpUpscaled)
    cmdKitty.Stdout = os.Stdout
    // redirect stderr to /dev/null to suppress kitty's imagemagick errors
    devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
    if err == nil {
        cmdKitty.Stderr = devNull
        defer devNull.Close()
    }
    
    if err := cmdKitty.Run(); err != nil {
        log.Printf("failed to display %s in Kitty: %v", tmpUpscaled, err)
        return
    }

    // remove temporary upscaled file
    _ = os.Remove(tmpUpscaled)
}

func fetchUser(client *http.Client, username string) User {
    url := fmt.Sprintf("https://capes.me/api/user/%s", username)
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("User-Agent", "Mozilla/5.0")

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
    cacheFile := filepath.Join(capeDir, "capes.json")
    var cached []Cape

    info, err := os.Stat(cacheFile)
    if err == nil && time.Since(info.ModTime()) < capeCacheTTL {
        f, err := os.Open(cacheFile)
        if err == nil {
            defer f.Close()
            _ = json.NewDecoder(f).Decode(&cached)
            return cached
        }
    }

    req, _ := http.NewRequest("GET", "https://capes.me/api/capes", nil)
    req.Header.Set("User-Agent", "Mozilla/5.0")
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