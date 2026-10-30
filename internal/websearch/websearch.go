package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type SearchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

var (
	htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
	spaceRegex   = regexp.MustCompile(`\s+`)
)

func cleanText(s string) string {
	s = htmlTagRegex.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(spaceRegex.ReplaceAllString(s, " "))
}

// Search 执行网络搜索，返回精简的搜索结果列表（最多 5 条）。
func Search(ctx context.Context, query string) ([]SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, strings.NewReader("q="+url.QueryEscape(q)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	html := string(body)

	var results []SearchResult
	blocks := strings.Split(html, "class=\"result results_links")
	if len(blocks) <= 1 {
		blocks = strings.Split(html, "class=\"result__body")
	}

	for i := 1; i < len(blocks) && len(results) < 5; i++ {
		b := blocks[i]
		tIdx := strings.Index(b, "class=\"result__snippet")
		titleIdx := strings.Index(b, "class=\"result__a\"")
		if titleIdx == -1 {
			titleIdx = strings.Index(b, "class=\"result__title")
		}

		var title, snippet, targetURL string
		if titleIdx != -1 {
			sub := b[titleIdx:]
			if aEnd := strings.Index(sub, "</a>"); aEnd != -1 {
				title = cleanText(sub[:aEnd])
			}
		}
		if tIdx != -1 {
			sub := b[tIdx:]
			if aEnd := strings.Index(sub, "</a>"); aEnd != -1 {
				snippet = cleanText(sub[:aEnd])
			}
		}

		uIdx := strings.Index(b, "class=\"result__url\"")
		if uIdx != -1 {
			sub := b[uIdx:]
			if aEnd := strings.Index(sub, "</a>"); aEnd != -1 {
				targetURL = cleanText(sub[:aEnd])
			}
		}

		if snippet != "" || title != "" {
			if title == "" {
				title = "搜索结果 " + fmt.Sprintf("%d", len(results)+1)
			}
			if targetURL == "" {
				targetURL = "web"
			}
			if !strings.HasPrefix(targetURL, "http") {
				targetURL = "https://" + targetURL
			}
			results = append(results, SearchResult{
				Title:   title,
				Snippet: snippet,
				URL:     targetURL,
			})
		}
	}

	return results, nil
}

// FormatPrompt 将搜索结果组织为提示词上下文。
func FormatPrompt(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	now := time.Now().Format("2006-01-02 15:04:05")
	sb.WriteString(fmt.Sprintf("\n\n[联网实时搜索结果 (当前时间: %s)]:\n", now))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("[%d] 标题: %s\n    来源: %s\n    摘要: %s\n", i+1, r.Title, r.URL, r.Snippet))
	}
	sb.WriteString("\n请结合上述最新的实时搜索结果与你的知识回答用户的问题。在引用具体事实或数据时，请标明参考来源编号（例如 [1] 或 [2]）。\n")
	return sb.String()
}
