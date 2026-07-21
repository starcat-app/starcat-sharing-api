// Package render 生成仓库分享的 Open Graph PNG。
//
// 渲染器只消费已验证的 RepositoryPreview，不负责网络请求。使用纯 Go 图像栈，
// 保证 CGO_ENABLED=0 的 Fly.io 镜像和本地测试得到一致结果。
package render

import (
	"bytes"
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
}

// NewOGRenderer 创建纯 Go 字体 renderer。
func NewOGRenderer() (*OGRenderer, error) {
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
	titleFace, err := newFace(bold, 34)
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
	}, nil
}

// Render 生成 PNG bytes。字体无法显示的字符会被替换，避免输出乱码方块。
func (r *OGRenderer) Render(repository model.RepositoryPreview, avatar image.Image) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, ogWidth, ogHeight))
	drawBackground(canvas)
	drawConstellation(canvas)
	drawRoundedPanel(canvas, image.Rect(54, 48, 1226, 592), 28, color.RGBA{14, 23, 43, 238})
	drawPanelBorder(canvas, image.Rect(54, 48, 1226, 592), color.RGBA{87, 142, 255, 90})

	drawCircle(canvas, image.Pt(112, 105), 17, color.RGBA{34, 116, 255, 255})
	drawStarGlyph(canvas, image.Pt(112, 105), 10, color.RGBA{255, 255, 255, 255})
	r.drawText(canvas, r.brandFace, color.RGBA{226, 235, 255, 255}, 142, 116, "STARCAT / REPOSITORY FIELD NOTE")

	drawAvatar(canvas, avatar, repository.Owner, image.Rect(92, 184, 236, 328))
	title := sanitizeForFace(r.titleFace, repository.FullName)
	title = truncateToWidth(r.titleFace, title, 870)
	r.drawText(canvas, r.titleFace, color.RGBA{248, 250, 255, 255}, 278, 225, title)

	description := strings.TrimSpace(repository.Description)
	if description == "" {
		description = "A GitHub repository shared from Starcat."
	}
	description = sanitizeForFace(r.descriptionFace, description)
	lines := wrapText(r.descriptionFace, description, 850, 2)
	for index, line := range lines {
		r.drawText(canvas, r.descriptionFace, color.RGBA{177, 190, 218, 255}, 278, 277+index*42, line)
	}

	metaY := 446
	metaX := 94
	if repository.Language != "" {
		languageColor := languageDotColor(repository.Language)
		drawCircle(canvas, image.Pt(metaX+8, metaY-6), 7, languageColor)
		metaX += 27
		r.drawText(canvas, r.metaFace, color.RGBA{215, 225, 246, 255}, metaX, metaY, sanitizeForFace(r.metaFace, repository.Language))
		metaX += textWidth(r.metaFace, repository.Language) + 54
	}

	r.drawText(canvas, r.metaFace, color.RGBA{115, 174, 255, 255}, metaX, metaY, "★")
	metaX += 31
	stars := compactNumber(repository.Stars)
	r.drawText(canvas, r.metaFace, color.RGBA{215, 225, 246, 255}, metaX, metaY, stars)
	metaX += textWidth(r.metaFace, stars) + 54
	r.drawText(canvas, r.metaFace, color.RGBA{115, 174, 255, 255}, metaX, metaY, "FORK")
	metaX += 66
	r.drawText(canvas, r.metaFace, color.RGBA{215, 225, 246, 255}, metaX, metaY, compactNumber(repository.Forks))

	status := "PUBLIC"
	if repository.Archived {
		status = "ARCHIVED"
	} else if repository.Template {
		status = "TEMPLATE"
	}
	drawStatusPill(canvas, r.metaFace, 92, 505, status)
	r.drawText(canvas, r.metaFace, color.RGBA{128, 149, 190, 255}, 1010, 536, "starcat.ink")

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

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
