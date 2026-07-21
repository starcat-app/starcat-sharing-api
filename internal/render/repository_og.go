// Package render 生成仓库分享的 Open Graph PNG。
//
// 渲染器只消费已验证的 RepositoryPreview，不负责网络请求。使用纯 Go 图像栈，
// 保证 CGO_ENABLED=0 的 Fly.io 镜像和本地测试得到一致结果。
package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	ogWidth  = 1280
	ogHeight = 640
)

// starcatLogoPNG 与官网使用同一份正式 App Icon。将它嵌入二进制可避免 OG crawler
// 请求期间再依赖外部静态资源，也保证 Fly.io 与本地渲染结果一致。
//
//go:embed starcat-logo.png
var starcatLogoPNG []byte

// StarcatLogoPNG 返回只读内嵌品牌资源，供公开分享页复用同一份正式图标。
// 调用方只应写入 HTTP response，不应修改返回的底层 bytes。
func StarcatLogoPNG() []byte {
	return starcatLogoPNG
}

// RepositoryOGRenderer 是 handler 可替换测试的 PNG renderer。
type RepositoryOGRenderer interface {
	Render(repository model.RepositoryPreview, avatar image.Image) ([]byte, error)
}

// OGRenderer 生成 1280×640 的 Starcat 仓库卡片。
type OGRenderer struct {
	titleFace       font.Face
	descriptionFace font.Face
	metaFace        font.Face
	brandFace       font.Face
	brandLogo       image.Image
}

// NewOGRenderer 创建纯 Go 字体 renderer。
func NewOGRenderer() (*OGRenderer, error) {
	brandLogo, err := png.Decode(bytes.NewReader(starcatLogoPNG))
	if err != nil {
		return nil, fmt.Errorf("decode embedded Starcat logo: %w", err)
	}
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse regular font: %w", err)
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse bold font: %w", err)
	}
	newFace := func(parsed *opentype.Font, size float64) (font.Face, error) {
		return opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 144, Hinting: font.HintingFull})
	}
	titleFace, err := newFace(bold, 30)
	if err != nil {
		return nil, err
	}
	descriptionFace, err := newFace(regular, 18)
	if err != nil {
		return nil, err
	}
	metaFace, err := newFace(bold, 15)
	if err != nil {
		return nil, err
	}
	brandFace, err := newFace(bold, 17)
	if err != nil {
		return nil, err
	}
	return &OGRenderer{
		titleFace:       titleFace,
		descriptionFace: descriptionFace,
		metaFace:        metaFace,
		brandFace:       brandFace,
		brandLogo:       brandLogo,
	}, nil
}

// Render 生成 PNG bytes。字体无法显示的字符会被替换，避免输出乱码方块。
func (r *OGRenderer) Render(repository model.RepositoryPreview, avatar image.Image) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, ogWidth, ogHeight))
	drawBackground(canvas)
	drawConstellation(canvas)
	drawRoundedPanel(canvas, image.Rect(54, 48, 1226, 592), 24, color.RGBA{13, 22, 39, 246})
	drawPanelBorder(canvas, image.Rect(54, 48, 1226, 592), color.RGBA{111, 151, 226, 82})

	drawImageFit(canvas, r.brandLogo, image.Rect(88, 76, 152, 140))
	r.drawText(canvas, r.brandFace, color.RGBA{241, 246, 255, 255}, 172, 103, "STARCAT")
	r.drawText(canvas, r.metaFace, color.RGBA{119, 139, 176, 255}, 172, 132, "Repository Share")
	visibilityLabel := "PUBLIC REPOSITORY"
	visibilityX := 1192 - textWidth(r.metaFace, visibilityLabel)
	drawCircle(canvas, image.Pt(visibilityX-17, 104), 6, color.RGBA{63, 185, 80, 255})
	r.drawText(canvas, r.metaFace, color.RGBA{139, 158, 193, 255}, visibilityX, 111, visibilityLabel)
	drawLine(canvas, image.Pt(88, 166), image.Pt(1192, 166), color.RGBA{106, 129, 170, 52})

	drawAvatar(canvas, avatar, repository.Owner, image.Rect(90, 216, 202, 328))
	r.drawText(canvas, r.metaFace, color.RGBA{126, 174, 255, 255}, 234, 220, "github.com/"+sanitizeForFace(r.metaFace, repository.Owner))
	title := sanitizeForFace(r.titleFace, repository.FullName)
	title = truncateToWidth(r.titleFace, title, 920)
	r.drawText(canvas, r.titleFace, color.RGBA{248, 250, 255, 255}, 234, 278, title)

	description := strings.TrimSpace(repository.Description)
	if description == "" {
		description = "A GitHub repository shared from Starcat."
	}
	description = sanitizeForFace(r.descriptionFace, description)
	lines := wrapText(r.descriptionFace, description, 900, 2)
	for index, line := range lines {
		r.drawText(canvas, r.descriptionFace, color.RGBA{174, 188, 213, 255}, 234, 326+index*40, line)
	}

	metaY := 464
	metaX := 92
	if repository.Language != "" {
		languageColor := languageDotColor(repository.Language)
		drawCircle(canvas, image.Pt(metaX+8, metaY-7), 7, languageColor)
		metaX += 27
		r.drawText(canvas, r.metaFace, color.RGBA{220, 228, 242, 255}, metaX, metaY, sanitizeForFace(r.metaFace, repository.Language))
		metaX += textWidth(r.metaFace, repository.Language) + 54
	} else {
		drawCircle(canvas, image.Pt(metaX+8, metaY-7), 7, color.RGBA{91, 107, 135, 255})
		metaX += 27
		language := "Language not detected"
		r.drawText(canvas, r.metaFace, color.RGBA{139, 155, 183, 255}, metaX, metaY, language)
		metaX += textWidth(r.metaFace, language) + 54
	}

	drawStarGlyph(canvas, image.Pt(metaX+9, metaY-9), 9, color.RGBA{242, 204, 96, 255})
	metaX += 29
	stars := compactNumber(repository.Stars)
	starLabel := stars + " stars"
	r.drawText(canvas, r.metaFace, color.RGBA{220, 228, 242, 255}, metaX, metaY, starLabel)
	metaX += textWidth(r.metaFace, starLabel) + 54
	drawForkGlyph(canvas, image.Pt(metaX+8, metaY-9), color.RGBA{139, 162, 201, 255})
	metaX += 29
	r.drawText(canvas, r.metaFace, color.RGBA{220, 228, 242, 255}, metaX, metaY, compactNumber(repository.Forks)+" forks")

	status := "PUBLIC"
	if repository.Archived {
		status = "ARCHIVED"
	} else if repository.Template {
		status = "TEMPLATE"
	}
	drawStatusPill(canvas, r.metaFace, 92, 514, status)
	footerLabel := "starcat.ink"
	r.drawText(canvas, r.metaFace, color.RGBA{128, 149, 190, 255}, 1192-textWidth(r.metaFace, footerLabel), 540, footerLabel)

	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, fmt.Errorf("encode repository OG PNG: %w", err)
	}
	return output.Bytes(), nil
}

func drawBackground(dst *image.RGBA) {
	for y := 0; y < ogHeight; y++ {
		t := float64(y) / float64(ogHeight)
		for x := 0; x < ogWidth; x++ {
			glow := math.Max(0, 1-math.Hypot(float64(x-1040)/750, float64(y-60)/520))
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(4 + 8*t + 8*glow),
				G: uint8(10 + 12*t + 22*glow),
				B: uint8(24 + 18*t + 52*glow),
				A: 255,
			})
		}
	}
}

func drawConstellation(dst *image.RGBA) {
	points := []image.Point{{1010, 95}, {1100, 128}, {1164, 88}, {1088, 215}, {1190, 256}, {990, 288}}
	for index := 0; index < len(points)-1; index++ {
		drawLine(dst, points[index], points[index+1], color.RGBA{72, 133, 255, 42})
	}
	for _, point := range points {
		drawCircle(dst, point, 3, color.RGBA{126, 179, 255, 150})
	}
}

func drawRoundedPanel(dst *image.RGBA, rect image.Rectangle, radius int, fill color.RGBA) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if insideRoundedRect(x, y, rect, radius) {
				dst.SetRGBA(x, y, fill)
			}
		}
	}
}

func drawPanelBorder(dst *image.RGBA, rect image.Rectangle, border color.RGBA) {
	for x := rect.Min.X + 28; x < rect.Max.X-28; x++ {
		dst.SetRGBA(x, rect.Min.Y, border)
		dst.SetRGBA(x, rect.Max.Y-1, border)
	}
	for y := rect.Min.Y + 28; y < rect.Max.Y-28; y++ {
		dst.SetRGBA(rect.Min.X, y, border)
		dst.SetRGBA(rect.Max.X-1, y, border)
	}
}

func insideRoundedRect(x, y int, rect image.Rectangle, radius int) bool {
	if x >= rect.Min.X+radius && x < rect.Max.X-radius || y >= rect.Min.Y+radius && y < rect.Max.Y-radius {
		return true
	}
	cx := rect.Min.X + radius
	if x >= rect.Max.X-radius {
		cx = rect.Max.X - radius - 1
	}
	cy := rect.Min.Y + radius
	if y >= rect.Max.Y-radius {
		cy = rect.Max.Y - radius - 1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

// drawImageFit 将品牌图标缩放到固定区域。图标自身已包含圆角和透明边缘，
// 这里不再额外裁切，避免破坏正式 App Icon 的玻璃质感。
func drawImageFit(dst *image.RGBA, source image.Image, rect image.Rectangle) {
	if source == nil {
		return
	}
	xdraw.CatmullRom.Scale(dst, rect, source, source.Bounds(), draw.Over, nil)
}

func drawAvatar(dst *image.RGBA, source image.Image, owner string, rect image.Rectangle) {
	if source == nil {
		drawCircle(dst, image.Pt((rect.Min.X+rect.Max.X)/2, (rect.Min.Y+rect.Max.Y)/2), rect.Dx()/2, color.RGBA{32, 69, 132, 255})
		initial := "?"
		if owner != "" {
			initial = strings.ToUpper(string([]rune(owner)[0]))
		}
		face, _ := opentype.Parse(gobold.TTF)
		initialFace, _ := opentype.NewFace(face, &opentype.FaceOptions{Size: 44, DPI: 144})
		drawer := font.Drawer{Dst: dst, Src: image.NewUniform(color.White), Face: initialFace}
		width := drawer.MeasureString(initial).Round()
		drawer.Dot = fixed.P((rect.Min.X+rect.Max.X-width)/2, rect.Min.Y+96)
		drawer.DrawString(initial)
		return
	}

	resized := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), source, source.Bounds(), draw.Over, nil)
	centerX, centerY := rect.Dx()/2, rect.Dy()/2
	radius := rect.Dx() / 2
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			dx, dy := x-centerX, y-centerY
			if dx*dx+dy*dy <= radius*radius {
				dst.Set(rect.Min.X+x, rect.Min.Y+y, resized.At(x, y))
			}
		}
	}
}

func (r *OGRenderer) drawText(dst draw.Image, face font.Face, ink color.Color, x, y int, value string) {
	drawer := font.Drawer{Dst: dst, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(x, y)}
	drawer.DrawString(value)
}

func wrapText(face font.Face, value string, maxWidth, maxLines int) []string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, maxLines)
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if textWidth(face, candidate) <= maxWidth {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
		if len(lines) == maxLines-1 {
			break
		}
	}
	if len(lines) < maxLines {
		lines = append(lines, truncateToWidth(face, current, maxWidth))
	}
	if len(lines) == maxLines && len(words) > 1 {
		lines[maxLines-1] = truncateToWidth(face, lines[maxLines-1]+"…", maxWidth)
	}
	return lines
}

func truncateToWidth(face font.Face, value string, maxWidth int) string {
	if textWidth(face, value) <= maxWidth {
		return value
	}
	runes := []rune(value)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if textWidth(face, candidate) <= maxWidth {
			return candidate
		}
	}
	return "…"
}

func sanitizeForFace(face font.Face, value string) string {
	var output strings.Builder
	output.Grow(len(value))
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		if _, _, ok := face.GlyphBounds(r); ok {
			output.WriteRune(r)
		} else {
			output.WriteRune('·')
		}
	}
	return output.String()
}

func textWidth(face font.Face, value string) int {
	drawer := font.Drawer{Face: face}
	return drawer.MeasureString(value).Round()
}

func compactNumber(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func languageDotColor(language string) color.RGBA {
	// 常见语言使用 GitHub 用户熟悉的颜色，并与 repository.html 保持一致。
	known := map[string]color.RGBA{
		"Swift":      {240, 81, 56, 255},
		"Go":         {0, 173, 216, 255},
		"JavaScript": {241, 224, 90, 255},
		"TypeScript": {49, 120, 198, 255},
		"Python":     {53, 114, 165, 255},
		"Rust":       {222, 165, 132, 255},
		"Kotlin":     {169, 123, 255, 255},
		"Java":       {176, 114, 25, 255},
		"C++":        {243, 75, 125, 255},
	}
	if value, ok := known[language]; ok {
		return value
	}
	colors := []color.RGBA{
		{49, 120, 255, 255}, {62, 207, 142, 255}, {255, 174, 62, 255},
		{245, 99, 126, 255}, {101, 192, 255, 255}, {183, 127, 255, 255},
	}
	hash := 0
	for _, r := range language {
		hash = (hash*31 + int(r)) & 0x7fffffff
	}
	return colors[hash%len(colors)]
}

func drawStatusPill(dst *image.RGBA, face font.Face, x, y int, value string) {
	width := textWidth(face, value) + 34
	drawRoundedPanel(dst, image.Rect(x, y, x+width, y+34), 17, color.RGBA{27, 65, 124, 255})
	drawer := font.Drawer{Dst: dst, Src: image.NewUniform(color.RGBA{151, 192, 255, 255}), Face: face, Dot: fixed.P(x+17, y+24)}
	drawer.DrawString(value)
}

func drawCircle(dst *image.RGBA, center image.Point, radius int, fill color.RGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				dst.SetRGBA(center.X+x, center.Y+y, fill)
			}
		}
	}
}

func drawLine(dst *image.RGBA, start, end image.Point, ink color.RGBA) {
	steps := int(math.Max(math.Abs(float64(end.X-start.X)), math.Abs(float64(end.Y-start.Y))))
	if steps == 0 {
		return
	}
	for index := 0; index <= steps; index++ {
		t := float64(index) / float64(steps)
		x := int(float64(start.X) + float64(end.X-start.X)*t)
		y := int(float64(start.Y) + float64(end.Y-start.Y)*t)
		dst.SetRGBA(x, y, ink)
	}
}

func drawStarGlyph(dst *image.RGBA, center image.Point, radius int, ink color.RGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if abs(x)+abs(y) <= radius || abs(x) <= 2 || abs(y) <= 2 {
				dst.SetRGBA(center.X+x, center.Y+y, ink)
			}
		}
	}
}

// drawForkGlyph 使用简单节点与连线绘制 Git fork 语义，避免用 "FORK" 文本冒充图标。
// 坐标围绕 center 设计为 18×18，可与 Star 和语言色点保持同一视觉重量。
func drawForkGlyph(dst *image.RGBA, center image.Point, ink color.RGBA) {
	left := image.Pt(center.X-5, center.Y-6)
	right := image.Pt(center.X+5, center.Y-6)
	bottom := image.Pt(center.X, center.Y+6)
	joinY := center.Y
	drawLine(dst, image.Pt(left.X, left.Y+2), image.Pt(left.X, joinY), ink)
	drawLine(dst, image.Pt(right.X, right.Y+2), image.Pt(right.X, joinY), ink)
	drawLine(dst, image.Pt(left.X, joinY), image.Pt(right.X, joinY), ink)
	drawLine(dst, image.Pt(center.X, joinY), image.Pt(center.X, bottom.Y-2), ink)
	drawCircle(dst, left, 2, ink)
	drawCircle(dst, right, 2, ink)
	drawCircle(dst, bottom, 2, ink)
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
