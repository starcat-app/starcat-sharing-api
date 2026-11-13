// 本文件只处理受限的 1600×840 PNG，不下载外部图片，也不接受文件路径。
package render

import (
	"bytes"
	"context"
	"errors"
	"image/png"

	"github.com/gen2brain/vpx/webp"
)

const (
	DailyRadarHeroWidth  = 1600
	DailyRadarHeroHeight = 840
	DailyRadarPNGMax     = 3 << 20
	DailyRadarWebPMax    = 1 << 20
)

var (
	ErrHeroTooLarge = errors.New("hero exceeds size limit")
	ErrHeroFormat   = errors.New("hero must be a 1600x840 PNG")
)

// DailyRadarHero 同时持有公开格式，调用方只有在两者都成功后才发布快照。
type DailyRadarHero struct {
	WebP []byte
	PNG  []byte
}

// EncodeDailyRadarHero 先验证 PNG header 的像素数，避免先解码任意尺寸图片再检查。
//
// 纯 Go VP8 编码无需 cgo、系统 libwebp 或子进程；质量固定 88，超过 1 MB 仅再试 84。
// context 在每个编码阶段之间检查；调用方另用单槽限制同时工作的编码器数量。
func EncodeDailyRadarHero(ctx context.Context, uploaded []byte) (DailyRadarHero, error) {
	if len(uploaded) > DailyRadarPNGMax {
		return DailyRadarHero{}, ErrHeroTooLarge
	}
	config, err := png.DecodeConfig(bytes.NewReader(uploaded))
	if err != nil || config.Width != DailyRadarHeroWidth || config.Height != DailyRadarHeroHeight {
		return DailyRadarHero{}, ErrHeroFormat
	}
	if err = ctx.Err(); err != nil {
		return DailyRadarHero{}, err
	}
	image, err := png.Decode(bytes.NewReader(uploaded))
	if err != nil {
		return DailyRadarHero{}, ErrHeroFormat
	}
	var fallback bytes.Buffer
	if err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&fallback, image); err != nil {
		return DailyRadarHero{}, err
	}
	if fallback.Len() > DailyRadarPNGMax {
		return DailyRadarHero{}, ErrHeroTooLarge
	}
	for _, quality := range []int{88, 84} {
		if err = ctx.Err(); err != nil {
			return DailyRadarHero{}, err
		}
		var encoded bytes.Buffer
		if err = webp.Encode(&encoded, image, webp.EncodeOptions{Quality: quality}); err != nil {
			return DailyRadarHero{}, err
		}
		if encoded.Len() <= DailyRadarWebPMax {
			return DailyRadarHero{WebP: encoded.Bytes(), PNG: fallback.Bytes()}, ctx.Err()
		}
	}
	return DailyRadarHero{}, ErrHeroTooLarge
}
