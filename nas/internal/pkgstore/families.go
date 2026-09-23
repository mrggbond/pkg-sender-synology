package pkgstore

import (
	"sort"
	"strings"
)

type Family struct {
	ID             string    `json:"id"`
	TitleID        string    `json:"titleId,omitempty"`
	Title          string    `json:"title"`
	SecondaryTitle string    `json:"secondaryTitle,omitempty"`
	Platform       string    `json:"platform,omitempty"`
	PackageCount   int       `json:"packageCount"`
	TotalSize      int64     `json:"totalSize"`
	Packages       []Package `json:"packages"`
}

func (s *Store) Families() []Family {
	return BuildFamilies(s.List())
}

func BuildFamilies(packages []Package) []Family {
	type familyBuilder struct {
		family    Family
		titleRank int
	}

	builders := make(map[string]*familyBuilder)
	for _, pkg := range packages {
		key, familyID := familyKey(pkg)
		builder := builders[key]
		if builder == nil {
			builder = &familyBuilder{
				family: Family{
					ID:       familyID,
					TitleID:  pkg.TitleID,
					Platform: pkg.Platform,
					Packages: make([]Package, 0, 1),
				},
				titleRank: 100,
			}
			builders[key] = builder
		}

		builder.family.Packages = append(builder.family.Packages, pkg)
		builder.family.PackageCount++
		builder.family.TotalSize += pkg.Size
		if builder.family.TitleID == "" && pkg.TitleID != "" {
			builder.family.TitleID = pkg.TitleID
		}
		if builder.family.Platform == "" && pkg.Platform != "" {
			builder.family.Platform = pkg.Platform
		}

		rank := familyTitleRank(pkg)
		if title := packageDisplayTitle(pkg); title != "" && rank < builder.titleRank {
			builder.family.Title = title
			builder.family.SecondaryTitle = packageSecondaryTitle(pkg, title)
			builder.titleRank = rank
		}
	}

	families := make([]Family, 0, len(builders))
	for _, builder := range builders {
		family := builder.family
		sort.Slice(family.Packages, func(i, j int) bool {
			ri, rj := packageTypeRank(family.Packages[i].PackageType), packageTypeRank(family.Packages[j].PackageType)
			if ri != rj {
				return ri < rj
			}
			if family.Packages[i].Version != family.Packages[j].Version {
				return family.Packages[i].Version > family.Packages[j].Version
			}
			return strings.ToLower(family.Packages[i].RelativePath) < strings.ToLower(family.Packages[j].RelativePath)
		})
		if family.Title == "" {
			family.Title = familyFallbackTitle(family.Packages[0])
		}
		families = append(families, family)
	}

	sort.Slice(families, func(i, j int) bool {
		left, right := strings.ToLower(families[i].Title), strings.ToLower(families[j].Title)
		if left != right {
			return left < right
		}
		return families[i].ID < families[j].ID
	})
	return families
}

func familyKey(pkg Package) (string, string) {
	if titleID := strings.ToUpper(strings.TrimSpace(pkg.TitleID)); titleID != "" {
		return "title:" + titleID, titleID
	}
	return "package:" + pkg.ID, pkg.ID
}

func familyTitleRank(pkg Package) int {
	if packageDisplayTitle(pkg) == "" {
		return 100
	}
	switch pkg.PackageType {
	case "game":
		return 0
	case "patch":
		return 10
	case "app":
		return 20
	case "dlc":
		return 30
	default:
		return 40
	}
}

func familyFallbackTitle(pkg Package) string {
	if title := packageDisplayTitle(pkg); title != "" {
		return title
	}
	if name := strings.TrimSpace(pkg.Name); name != "" {
		return name
	}
	return pkg.ID
}

func packageDisplayTitle(pkg Package) string {
	if title := strings.TrimSpace(pkg.DisplayTitle); title != "" {
		return title
	}
	return strings.TrimSpace(pkg.Title)
}

func packageSecondaryTitle(pkg Package, primary string) string {
	secondary := strings.TrimSpace(pkg.SecondaryTitle)
	if secondary == "" || strings.EqualFold(secondary, strings.TrimSpace(primary)) {
		return ""
	}
	return secondary
}

func packageTypeRank(packageType string) int {
	switch packageType {
	case "game":
		return 0
	case "patch":
		return 10
	case "dlc":
		return 20
	case "app":
		return 30
	default:
		return 40
	}
}
