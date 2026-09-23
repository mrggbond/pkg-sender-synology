package pkgstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Loopayeh/pkg-sender/nas/internal/pkgmeta"
)

type TitleAliases map[string]map[string]string

type MissingTitleAlias struct {
	TitleID            string   `json:"titleId,omitempty"`
	ContentID          string   `json:"contentId,omitempty"`
	DisplayTitle       string   `json:"displayTitle,omitempty"`
	Title              string   `json:"title,omitempty"`
	PackageCount       int      `json:"packageCount"`
	LocalizedLanguages []string `json:"localizedLanguages,omitempty"`
}

type TitleAliasExport struct {
	Missing       []MissingTitleAlias `json:"missing"`
	AliasTemplate map[string]any      `json:"aliasTemplate"`
	AIPrompt      string              `json:"aiPrompt"`
}

type TitleAliasImportResult struct {
	Titles    int `json:"titles"`
	Languages int `json:"languages"`
}

func LoadTitleAliasesFile(path string) (TitleAliases, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return TitleAliases{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return TitleAliases{}, nil
	}
	if err != nil {
		return nil, err
	}
	aliases, err := parseTitleAliasesJSON(data)
	if err != nil {
		return nil, err
	}
	return aliases.normalized(), nil
}

func parseTitleAliasesJSON(data []byte) (TitleAliases, error) {
	raw, err := extractTitleAliasObject(data)
	if err != nil {
		return nil, err
	}
	return parseTitleAliasesRaw(raw)
}

func extractTitleAliasObject(data []byte) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if value, ok := raw["aliasTemplate"]; ok {
		var template map[string]json.RawMessage
		if err := json.Unmarshal(value, &template); err != nil {
			return nil, fmt.Errorf("aliasTemplate must be an object: %w", err)
		}
		return template, nil
	}
	return raw, nil
}

func parseTitleAliasesRaw(raw map[string]json.RawMessage) (TitleAliases, error) {
	aliases := make(TitleAliases, len(raw))
	for key, value := range raw {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}
		var titles map[string]string
		if err := json.Unmarshal(value, &titles); err != nil {
			return nil, fmt.Errorf("title alias %q must be an object of language titles: %w", key, err)
		}
		aliases[key] = titles
	}
	return aliases, nil
}

func ImportTitleAliasesFile(path string, data []byte) (TitleAliases, TitleAliasImportResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, TitleAliasImportResult{}, errors.New("title aliases file path is not configured")
	}
	incomingRaw, err := extractTitleAliasObject(data)
	if err != nil {
		return nil, TitleAliasImportResult{}, err
	}
	incoming, err := parseTitleAliasesRaw(incomingRaw)
	if err != nil {
		return nil, TitleAliasImportResult{}, err
	}
	existing, err := LoadTitleAliasesFile(path)
	if err != nil {
		return nil, TitleAliasImportResult{}, err
	}
	merged := existing.normalized()
	if merged == nil {
		merged = TitleAliases{}
	}
	for key, values := range incoming.normalized() {
		if len(values) == 0 {
			continue
		}
		key = normalizeAliasKey(key)
		current := merged[key]
		if current == nil {
			current = map[string]string{}
			merged[key] = current
		}
		for lang, title := range values {
			current[lang] = title
		}
	}
	prompt := extractAIPrompt(data, incomingRaw)
	if prompt == "" {
		prompt = extractExistingAIPrompt(path)
	}
	if err := writeTitleAliasesFile(path, merged, prompt); err != nil {
		return nil, TitleAliasImportResult{}, err
	}
	result := TitleAliasImportResult{}
	for _, values := range merged {
		if len(values) == 0 {
			continue
		}
		result.Titles++
		result.Languages += len(values)
	}
	return merged, result, nil
}

func extractAIPrompt(data []byte, aliasRaw map[string]json.RawMessage) string {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err == nil {
		if value, ok := root["aiPrompt"]; ok {
			var prompt string
			if json.Unmarshal(value, &prompt) == nil {
				if prompt = strings.TrimSpace(prompt); prompt != "" {
					return prompt
				}
			}
		}
	}
	if value, ok := aliasRaw["_aiPrompt"]; ok {
		var prompt string
		if json.Unmarshal(value, &prompt) == nil {
			return strings.TrimSpace(prompt)
		}
	}
	return ""
}

func extractExistingAIPrompt(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	raw, err := extractTitleAliasObject(data)
	if err != nil {
		return ""
	}
	if value, ok := raw["_aiPrompt"]; ok {
		var prompt string
		if json.Unmarshal(value, &prompt) == nil {
			return strings.TrimSpace(prompt)
		}
	}
	return ""
}

func writeTitleAliasesFile(path string, aliases TitleAliases, prompt string) error {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := make(map[string]any, len(aliases)+1)
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		body["_aiPrompt"] = prompt
	}
	for key, values := range aliases.normalized() {
		if len(values) != 0 {
			body[key] = values
		}
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".aliases-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	ok = true
	return nil
}

func (a TitleAliases) normalized() TitleAliases {
	if len(a) == 0 {
		return TitleAliases{}
	}
	out := make(TitleAliases, len(a))
	for key, values := range a {
		key = normalizeAliasKey(key)
		if key == "" || len(values) == 0 {
			continue
		}
		next := make(map[string]string, len(values))
		for lang, title := range values {
			lang = strings.TrimSpace(lang)
			title = strings.TrimSpace(title)
			if lang != "" && title != "" {
				next[lang] = title
			}
		}
		if len(next) != 0 {
			out[key] = next
		}
	}
	return out
}

func (a TitleAliases) apply(meta *pkgmeta.Metadata) {
	if len(a) == 0 || meta == nil || strings.TrimSpace(meta.SecondaryTitle) != "" {
		return
	}
	alias := a.find(meta.TitleID, meta.ContentID)
	secondary := firstAliasTitle(alias)
	if secondary != "" && !strings.EqualFold(strings.TrimSpace(secondary), strings.TrimSpace(meta.DisplayTitle)) {
		meta.SecondaryTitle = secondary
	}
}

func (a TitleAliases) find(titleID, contentID string) map[string]string {
	for _, key := range []string{titleID, contentID} {
		if values := a[normalizeAliasKey(key)]; len(values) != 0 {
			return values
		}
	}
	return nil
}

func firstAliasTitle(values map[string]string) string {
	for _, key := range []string{"zh-Hant", "zh-HK", "zh-TW", "zh-Hans", "zh-CN", "zh-SG", "zh"} {
		if title := strings.TrimSpace(values[key]); title != "" {
			return title
		}
	}
	return ""
}

func normalizeAliasKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}

func BuildTitleAliasExport(missing []MissingTitleAlias) TitleAliasExport {
	missing = append([]MissingTitleAlias(nil), missing...)
	if missing == nil {
		missing = []MissingTitleAlias{}
	}
	prompt := buildTitleAliasPrompt(missing)
	template := make(map[string]any, len(missing)+1)
	template["_aiPrompt"] = prompt
	for _, item := range missing {
		key := missingAliasKey(item)
		if key == "" {
			continue
		}
		template[key] = map[string]string{
			"zh-Hans": "",
			"zh-Hant": "",
		}
	}
	return TitleAliasExport{
		Missing:       missing,
		AliasTemplate: template,
		AIPrompt:      prompt,
	}
}

func buildTitleAliasPrompt(missing []MissingTitleAlias) string {
	template := make(map[string]any, len(missing)+1)
	template["_aiPrompt"] = "保留或替换为 GET /api/title-alias-export 返回的 aiPrompt；程序会忽略这个字段。"
	for _, item := range missing {
		key := missingAliasKey(item)
		if key == "" {
			continue
		}
		template[key] = map[string]string{"zh-Hans": "", "zh-Hant": ""}
	}
	missingJSON, _ := json.MarshalIndent(missing, "", "  ")
	templateJSON, _ := json.MarshalIndent(template, "", "  ")
	return strings.TrimSpace(`请根据下面的 PS5 Title ID / Content ID / 英文标题，查找对应游戏的简体中文名称，并填写 aliases.json。

要求：
1. 只返回 JSON，不要解释。
2. JSON 顶层 key 必须使用给定的 titleId；如果没有 titleId 才使用 contentId。
3. 每个条目至少填写 zh-Hans。只有确定繁体中文官方/常用名称时才填写 zh-Hant；不确定可留空字符串。
4. 不要翻译 DLC 解锁器、补丁、版本号、发布组名称；无法确认时保留空字符串。
5. 不要删除 _aiPrompt 字段；它会被程序忽略，仅用于后续复制给 AI。

缺失别名列表：
` + string(missingJSON) + `

请按这个模板填写：
` + string(templateJSON))
}

func missingAliasKey(item MissingTitleAlias) string {
	if key := normalizeAliasKey(item.TitleID); key != "" {
		return key
	}
	return normalizeAliasKey(item.ContentID)
}

func localizedLanguageKeys(titles map[string]string) []string {
	keys := make([]string, 0, len(titles))
	for key := range titles {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
