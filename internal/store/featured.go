package store

import (
	"strings"

	"deepweay-chat/internal/models"
)

const FeaturedAppliedKey = "featured_v13_vultr_flagship_applied"

// ApplyFeaturedModels 关掉全部模型，只上架聊天页 8 款精选（Vultr 7 款 + xAI Grok 2 款）。
// 自动按 Hints 匹配服务商：Vultr 条目命中 vultrinference，xAI 条目命中 api.x.ai。
func (s *Store) ApplyFeaturedModels() (int, error) {
	providers, err := s.ListProviders()
	if err != nil {
		return 0, err
	}

	if _, err := s.db.Exec(`UPDATE models SET is_enabled = 0, updated_at = CURRENT_TIMESTAMP`); err != nil {
		return 0, err
	}

	pickProvider := func(hints []string) int64 {
		for _, h := range hints {
			hl := strings.ToLower(h)
			for _, p := range providers {
				blob := strings.ToLower(p.BaseURL + " " + p.Name + " " + p.Type)
				if strings.Contains(blob, hl) {
					return p.ID
				}
			}
		}
		return 0
	}

	n := 0
	for i, spec := range models.Featured {
		id := spec.ID
		raw := spec.RawID
		if raw == "" {
			raw = spec.ID
		}
		pid := pickProvider(spec.Hints)
		item := ModelItem{
			ID:          id,
			ProviderID:  pid,
			RawModelID:  raw,
			Name:        spec.Name,
			Vendor:      spec.Vendor,
			Tier:        spec.Tier,
			Description: models.FeaturedDescription(spec),
			IsEnabled:   true,
			SortOrder:   i + 1,
		}
		if err := s.SaveModel(&item); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// EnableOnlyModel 关掉其它模型，只留指定 id。已存在则改成启用并指向 xAI。
func (s *Store) EnableOnlyModel(id, raw, name, vendor string) error {
	providers, err := s.ListProviders()
	if err != nil {
		return err
	}
	var pid int64
	for _, p := range providers {
		if strings.Contains(strings.ToLower(p.BaseURL), "api.x.ai") && strings.TrimSpace(p.APIKey) != "" {
			pid = p.ID
			break
		}
	}
	if _, err := s.db.Exec(`UPDATE models SET is_enabled = 0, updated_at = CURRENT_TIMESTAMP`); err != nil {
		return err
	}
	item := ModelItem{
		ID: id, ProviderID: pid, RawModelID: raw, Name: name,
		Vendor: vendor, Tier: "standard", Description: "xAI 官方直连",
		IsEnabled: true, SortOrder: 1,
	}
	return s.SaveModel(&item)
}

// EnableFeaturedForProvider 只上架与该服务商匹配的精选模型，不影响其它渠道。
func (s *Store) EnableFeaturedForProvider(p *Provider) (int, error) {
	if p == nil || p.ID == 0 {
		return 0, nil
	}
	blob := strings.ToLower(p.BaseURL + " " + p.Name + " " + p.Type)
	n := 0
	for i, spec := range models.Featured {
		match := false
		for _, h := range spec.Hints {
			if strings.Contains(blob, strings.ToLower(h)) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		raw := spec.RawID
		if raw == "" {
			raw = spec.ID
		}
		item := ModelItem{
			ID:          spec.ID,
			ProviderID:  p.ID,
			RawModelID:  raw,
			Name:        spec.Name,
			Vendor:      spec.Vendor,
			Tier:        spec.Tier,
			Description: models.FeaturedDescription(spec),
			IsEnabled:   true,
			SortOrder:   i + 1,
		}
		if err := s.SaveModel(&item); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
