package render

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
)

func TestRepositoryOGRendererProducesExpectedPNG(t *testing.T) {
	renderer, err := NewOGRenderer()
	if err != nil {
		t.Fatalf("NewOGRenderer failed: %v", err)
	}
	if renderer.brandLogo == nil {
		t.Fatal("official Starcat logo must be embedded in the OG renderer")
	}
	data, err := renderer.Render(model.RepositoryPreview{
		Owner:       "starcat-app",
		Name:        "Starcat",
		FullName:    "starcat-app/Starcat",
		Description: "A searchable knowledge base for GitHub repositories.",
		Language:    "Swift",
		Stars:       12842,
		Forks:       420,
	}, nil)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG config: %v", err)
	}
	if config.Width != 1280 || config.Height != 640 {
		t.Fatalf("unexpected image dimensions %dx%d", config.Width, config.Height)
	}

	// 文档类仓库可能没有 GitHub primary language；该分支仍必须稳定输出 PNG。
	withoutLanguage, err := renderer.Render(model.RepositoryPreview{
		Owner:    "example",
		Name:     "docs-only",
		FullName: "example/docs-only",
	}, nil)
	if err != nil {
		t.Fatalf("Render without language failed: %v", err)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(withoutLanguage)); err != nil {
		t.Fatalf("decode no-language PNG config: %v", err)
	}
}
