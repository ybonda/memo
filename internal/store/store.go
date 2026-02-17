package store

import (
	"fmt"
	"strings"

	"github.com/yuri-bondarenko/memo/internal/config"
	"github.com/yuri-bondarenko/memo/internal/db"
	"github.com/yuri-bondarenko/memo/internal/embedding"
	"github.com/yuri-bondarenko/memo/internal/model"
)

type MemoryStore struct {
	db       *db.DB
	embedder *embedding.Embedder
	config   *config.Config
}

func New(cfg *config.Config) (*MemoryStore, error) {
	database, err := db.Open(cfg.DBPath, cfg.Embedding.Dimensions)
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	embedder, err := embedding.New(cfg.Embedding.Model, cfg.Embedding.CacheDir)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("cannot init embedder: %w", err)
	}

	return &MemoryStore{db: database, embedder: embedder, config: cfg}, nil
}

func (s *MemoryStore) Close() {
	if s.db != nil {
		s.db.Close()
	}
	if s.embedder != nil {
		s.embedder.Destroy()
	}
}

func (s *MemoryStore) Store(content string, tags []string, memType string) (*model.StoreResult, error) {
	if memType == "" {
		memType = s.config.DefaultType
	}
	if err := s.config.ValidateType(memType); err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}

	id := model.GenerateID(content)

	exists, err := s.db.Exists(id)
	if err != nil {
		return nil, err
	}
	if exists {
		return &model.StoreResult{ID: id, Status: "exists"}, nil
	}

	emb, err := s.embedder.Embed(content)
	if err != nil {
		return nil, err
	}

	similar, err := s.db.KNNSearch(emb, 1, nil)
	if err != nil {
		return nil, err
	}
	if len(similar) > 0 && similar[0].Score >= s.config.DuplicateThreshold {
		return &model.StoreResult{
			ID:            similar[0].ID,
			Status:        "similar_exists",
			SimilarMemory: &similar[0].Content,
		}, nil
	}

	now := model.NowRFC3339()
	mem := &model.Memory{
		ID:        id,
		Content:   content,
		Type:      memType,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.db.Insert(mem, emb); err != nil {
		return nil, err
	}

	return &model.StoreResult{ID: id, Status: "created"}, nil
}

func (s *MemoryStore) Search(query string, limit int, memType *string) ([]model.MemoryWithScore, error) {
	if limit <= 0 {
		limit = 5
	}
	if memType != nil {
		if err := s.config.ValidateType(*memType); err != nil {
			return nil, err
		}
	}

	emb, err := s.embedder.Embed(query)
	if err != nil {
		return nil, err
	}
	return s.db.KNNSearch(emb, limit, memType)
}

func (s *MemoryStore) Delete(id string) (*model.DeleteResult, error) {
	deleted, err := s.db.Delete(id)
	if err != nil {
		return nil, err
	}
	return &model.DeleteResult{Deleted: deleted, ID: id}, nil
}

func (s *MemoryStore) Update(id string, content *string, tags *[]string, memType *string) (*model.UpdateResult, error) {
	mem, err := s.db.Get(id)
	if err != nil {
		return nil, err
	}
	if mem == nil {
		return &model.UpdateResult{Updated: false}, nil
	}

	if content != nil {
		mem.Content = *content
	}
	if tags != nil {
		mem.Tags = *tags
	}
	if memType != nil {
		if err := s.config.ValidateType(*memType); err != nil {
			return nil, err
		}
		mem.Type = *memType
	}
	mem.UpdatedAt = model.NowRFC3339()

	emb, err := s.embedder.Embed(mem.Content)
	if err != nil {
		return nil, err
	}

	if err := s.db.Update(mem, emb); err != nil {
		return nil, err
	}

	return &model.UpdateResult{Updated: true, Memory: mem}, nil
}

func (s *MemoryStore) List(limit int, memType *string) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	if memType != nil {
		if err := s.config.ValidateType(*memType); err != nil {
			return nil, err
		}
	}
	return s.db.ListAll(limit, memType)
}

func (s *MemoryStore) FindSimilar(content string) ([]model.MemoryWithScore, error) {
	emb, err := s.embedder.Embed(content)
	if err != nil {
		return nil, err
	}
	return s.db.KNNSearch(emb, 5, nil)
}

func (s *MemoryStore) Recall(query string, limit int) (*model.RecallResult, error) {
	if limit <= 0 {
		limit = 5
	}

	memories, err := s.Search(query, limit, nil)
	if err != nil {
		return nil, err
	}

	var context string
	if len(memories) == 0 {
		context = "No relevant memories found."
	} else {
		lines := make([]string, len(memories))
		for i, m := range memories {
			tagsStr := "none"
			if len(m.Tags) > 0 {
				tagsStr = strings.Join(m.Tags, ", ")
			}
			lines[i] = fmt.Sprintf("%d. [%s] %s\n   Tags: %s\n   Score: %.2f",
				i+1, m.Type, m.Content, tagsStr, m.Score)
		}
		context = strings.Join(lines, "\n\n")
	}

	return &model.RecallResult{Context: context, Memories: memories}, nil
}
