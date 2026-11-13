// 本文件验证真实的纯 Go 编码器，而非只检查 MIME 字符串或文件后缀。
package render

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/image/webp"
)

// testHeroPNG 模拟有渐变和细线的程序化封面，避免纯色图掩盖编码体积与内存成本。
func testHeroPNG(t testing.TB) []byte {
	t.Helper()
	if fixture := os.Getenv("DAILY_RADAR_HERO_FIXTURE"); fixture != "" {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, DailyRadarHeroWidth, DailyRadarHeroHeight))
	for vertical := range DailyRadarHeroHeight {
		for horizontal := range DailyRadarHeroWidth {
			canvas.SetNRGBA(horizontal, vertical, color.NRGBA{
				R: uint8(horizontal / 8), G: uint8(vertical / 4),
				B: uint8((horizontal + vertical) / 12), A: 255,
			})
		}
	}
	var result bytes.Buffer
	if err := png.Encode(&result, canvas); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

// TestDailyRadarHeroClientFixture 消费 macOS 测试附件，默认跳过，不依赖远程报告或 GUI。
func TestDailyRadarHeroClientFixture(t *testing.T) {
	if os.Getenv("DAILY_RADAR_HERO_FIXTURE") == "" {
		t.Skip("set DAILY_RADAR_HERO_FIXTURE to an exported client PNG")
	}
	uploaded := testHeroPNG(t)
	started := time.Now()
	hero, err := EncodeDailyRadarHero(context.Background(), uploaded)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	original, err := png.Decode(bytes.NewReader(uploaded))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := webp.Decode(bytes.NewReader(hero.WebP))
	if err != nil || decoded.Bounds() != original.Bounds() {
		t.Fatalf("decoded image mismatch: %v", err)
	}
	planes, ok := decoded.(*image.YCbCr)
	if !ok {
		t.Fatalf("expected lossy VP8 color planes, got %T", decoded)
	}
	var squaredError float64
	for vertical := range DailyRadarHeroHeight {
		for horizontal := range DailyRadarHeroWidth {
			red, green, blue, _ := original.At(horizontal, vertical).RGBA()
			decodedRed, decodedGreen, decodedBlue, _ := dailyRadarWebPRGB(planes.YCbCrAt(horizontal, vertical)).RGBA()
			for _, difference := range []float64{
				float64(int(red>>8) - int(decodedRed>>8)),
				float64(int(green>>8) - int(decodedGreen>>8)),
				float64(int(blue>>8) - int(decodedBlue>>8)),
			} {
				squaredError += difference * difference
			}
		}
	}
	psnr := 10 * math.Log10(255*255/(squaredError/(1600*840*3)))
	t.Logf("client_png=%d webp=%d fallback_png=%d encode=%s PSNR=%.2f dB", len(uploaded), len(hero.WebP), len(hero.PNG), elapsed, psnr)
	if output := os.Getenv("DAILY_RADAR_HERO_OUTPUT"); output != "" {
		if err = os.MkdirAll(output, 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"hero.webp": hero.WebP, "hero.png": hero.PNG} {
			if err = os.WriteFile(filepath.Join(output, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if psnr < 30 {
		t.Fatalf("unexpectedly low image fidelity: PSNR %.2f dB", psnr)
	}
}

// dailyRadarWebPRGB 按 WebP 的 BT.601 studio range 转换独立解码器返回的原始平面。
// image.YCbCr.RGBA 使用 JPEG full range，直接比较会把正确封面误判为 27 dB；
// 此处的换算已与 macOS ImageIO 的 sRGB 解码结果交叉验证，不能降低质量门槛掩盖差异。
func dailyRadarWebPRGB(pixel color.YCbCr) color.NRGBA {
	luminance := 1.16438356 * (float64(pixel.Y) - 16)
	chromaBlue := float64(pixel.Cb) - 128
	chromaRed := float64(pixel.Cr) - 128
	channel := func(value float64) uint8 {
		return uint8(math.Round(math.Max(0, math.Min(255, value))))
	}
	return color.NRGBA{
		R: channel(luminance + 1.59602678*chromaRed),
		G: channel(luminance - 0.39176229*chromaBlue - 0.81296765*chromaRed),
		B: channel(luminance + 2.01723214*chromaBlue),
		A: 255,
	}
}

func TestDailyRadarHeroRealEncoder(t *testing.T) {
	hero, err := EncodeDailyRadarHero(context.Background(), testHeroPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	config, err := webp.DecodeConfig(bytes.NewReader(hero.WebP))
	if err != nil || config.Width != 1600 || config.Height != 840 {
		t.Fatalf("invalid WebP: config=%+v err=%v", config, err)
	}
	fallback, err := png.DecodeConfig(bytes.NewReader(hero.PNG))
	if err != nil || fallback.Width != 1600 || fallback.Height != 840 {
		t.Fatalf("invalid PNG fallback: %+v %v", fallback, err)
	}
	if len(hero.WebP) > DailyRadarWebPMax || len(hero.PNG) > DailyRadarPNGMax {
		t.Fatal("encoded assets exceed hard limits")
	}
	t.Logf("webp=%d bytes png=%d bytes", len(hero.WebP), len(hero.PNG))
}

func TestDailyRadarHeroRejectsInvalidInputs(t *testing.T) {
	var small bytes.Buffer
	_ = png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	for _, invalid := range [][]byte{[]byte("not PNG"), small.Bytes()} {
		if _, err := EncodeDailyRadarHero(context.Background(), invalid); err != ErrHeroFormat {
			t.Fatalf("want format rejection, got %v", err)
		}
	}
	if _, err := EncodeDailyRadarHero(context.Background(), make([]byte, DailyRadarPNGMax+1)); err != ErrHeroTooLarge {
		t.Fatalf("want size rejection, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EncodeDailyRadarHero(ctx, testHeroPNG(t)); err != context.Canceled {
		t.Fatalf("want cancellation, got %v", err)
	}
}

func BenchmarkDailyRadarHero(b *testing.B) {
	uploaded := testHeroPNG(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := EncodeDailyRadarHero(context.Background(), uploaded); err != nil {
			b.Fatal(err)
		}
	}
}
