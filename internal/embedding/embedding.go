package embedding

import (
	"fmt"
	"os"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

type Embedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
}

func New(modelName, cacheDir string) (*Embedder, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create model cache dir: %w", err)
	}

	session, err := hugot.NewGoSession()
	if err != nil {
		return nil, fmt.Errorf("cannot create hugot session: %w", err)
	}

	modelPath, err := hugot.DownloadModel(modelName, cacheDir, hugot.NewDownloadOptions())
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("cannot download model %s: %w", modelName, err)
	}

	pipeline, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
		ModelPath: modelPath,
		Name:      "embedder",
	})
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("cannot create embedding pipeline: %w", err)
	}

	return &Embedder{session: session, pipeline: pipeline}, nil
}

func (e *Embedder) Embed(text string) ([]float32, error) {
	result, err := e.pipeline.RunPipeline([]string{text})
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding generated")
	}
	return result.Embeddings[0], nil
}

func (e *Embedder) Destroy() error {
	if e.session != nil {
		return e.session.Destroy()
	}
	return nil
}
