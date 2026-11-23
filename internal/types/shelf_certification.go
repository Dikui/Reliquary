package types

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type ShelfStandard struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

// Standard and nonempty Region use library-configured keys.
type ShelfCertification struct {
	OwnerID             string `json:"owner_id"`
	Standard            string `json:"standard"`
	Year                int    `json:"year,omitempty"`
	CertificationObject string `json:"certification_object"`
	CellPartID          string `json:"cell_part_id,omitempty"`
	Region              string `json:"region"`
	Agency              string `json:"agency,omitempty"`
	Progress            string `json:"progress,omitempty"`
	Notes               string `json:"notes,omitempty"`
	StartDate           string `json:"start_date,omitempty"`
	// PlannedDate is the expected certification date; ObtainedDate is set
	// only once the certificate has actually been obtained.
	PlannedDate  string           `json:"planned_date,omitempty"`
	ObtainedDate string           `json:"obtained_date,omitempty"`
	IssuedDate   string           `json:"issued_date,omitempty"`
	Validity     string           `json:"validity,omitempty"`
	Number       string           `json:"number,omitempty"`
	CloudURL     string           `json:"cloud_url,omitempty"`
	Conditions   []ShelfCondition `json:"conditions"`
}

type ShelfCertificationInput struct {
	Name string `json:"name,omitempty"`
	ShelfCertification
}

type ShelfCertificationView struct {
	Entity        Entity             `json:"entity"`
	Certification ShelfCertification `json:"certification"`
	Status        string             `json:"status"`
	StandardName  string             `json:"standard_name"`
	RegionName    string             `json:"region_name"`
	Origin        string             `json:"origin"`
}

// ShelfCertificationFactValue adds the derived status and standard name to a certification fact.
type ShelfCertificationFactValue struct {
	ShelfCertification
	Status       string `json:"status"`
	StandardName string `json:"standard_name"`
}

const (
	ShelfCertificationDateConflict = "date_conflict"
	ShelfCertificationObtained     = "obtained"
	ShelfCertificationPaused       = "paused"
	ShelfCertificationDelayed      = "delayed"
	ShelfCertificationNotStarted   = "not_started"
	ShelfCertificationInProgress   = "in_progress"
	ShelfCertificationUnknown      = "unknown"
)

func ShelfCertificationStatuses() []string {
	return []string{
		ShelfCertificationDateConflict, ShelfCertificationObtained, ShelfCertificationPaused,
		ShelfCertificationDelayed, ShelfCertificationNotStarted, ShelfCertificationInProgress, ShelfCertificationUnknown,
	}
}

// ShelfCertificationStatus uses local calendar dates. A future actual date is
// a conflict as of today, while completion and delay are evaluated as of the query.
func ShelfCertificationStatus(record ShelfCertification, asOf, today time.Time) string {
	asOfDate := asOf.Local().Format(time.DateOnly)
	obtainedByAsOf := record.ObtainedDate != "" && record.ObtainedDate <= asOfDate
	completedWithoutDate := record.ObtainedDate == "" && (record.Progress == "已取证" || record.Progress == "已完成")
	switch {
	case record.ObtainedDate > today.Local().Format(time.DateOnly):
		return ShelfCertificationDateConflict
	case obtainedByAsOf || completedWithoutDate:
		return ShelfCertificationObtained
	case strings.Contains(record.Progress, "暂停"):
		return ShelfCertificationPaused
	case record.PlannedDate != "" && record.PlannedDate < asOfDate:
		return ShelfCertificationDelayed
	case record.Progress == "待启动" || record.Progress == "未启动":
		return ShelfCertificationNotStarted
	case record.Progress != "" || record.StartDate != "" || record.PlannedDate != "":
		return ShelfCertificationInProgress
	default:
		return ShelfCertificationUnknown
	}
}

func ValidateShelfCertification(value any) error {
	var record ShelfCertification
	if err := DecodeShelfValue(value, &record); err != nil {
		return fmt.Errorf("invalid certification record: %w", err)
	}
	if strings.TrimSpace(record.OwnerID) == "" {
		return fmt.Errorf("uncertain: certification owner cannot be identified")
	}
	if strings.TrimSpace(record.Standard) == "" || strings.TrimSpace(record.CertificationObject) == "" {
		return fmt.Errorf("certification standard and object are required")
	}
	if record.Year < 0 || record.Year > 9999 {
		return fmt.Errorf("invalid certification year")
	}
	for _, value := range []string{record.OwnerID, record.Standard, record.CertificationObject, record.CellPartID, record.Region, record.Agency, record.Progress, record.Notes, record.Validity, record.Number, record.CloudURL} {
		if len([]rune(value)) > 20000 {
			return fmt.Errorf("certification text is too long")
		}
	}
	for _, date := range []string{record.StartDate, record.PlannedDate, record.ObtainedDate, record.IssuedDate} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return fmt.Errorf("certification dates must use YYYY-MM-DD")
			}
		}
	}
	if record.CloudURL != "" {
		link, err := url.Parse(record.CloudURL)
		if err != nil || (link.Scheme != "http" && link.Scheme != "https") || link.Hostname() == "" {
			return fmt.Errorf("certification cloud_url must be an HTTP(S) link")
		}
	}
	return ValidateShelfConditions(record.Conditions)
}

func (config ShelfConfig) Standard(value string) (ShelfStandard, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, standard := range config.Standards {
		for _, name := range append([]string{standard.Key, standard.Name}, standard.Aliases...) {
			if strings.ToLower(strings.TrimSpace(name)) == normalized {
				return standard, true
			}
		}
	}
	return ShelfStandard{}, false
}

func validateShelfStandards(standards []ShelfStandard) error {
	if len(standards) > 500 {
		return fmt.Errorf("at most 500 standards are allowed")
	}
	names := map[string]string{}
	keys := map[string]bool{}
	for _, standard := range standards {
		if !entitySchemaKey.MatchString(standard.Key) || keys[standard.Key] || strings.TrimSpace(standard.Name) == "" || len(standard.Aliases) > 200 {
			return fmt.Errorf("standards require unique keys, a name and at most 200 aliases")
		}
		keys[standard.Key] = true
		for _, name := range append([]string{standard.Key, standard.Name}, standard.Aliases...) {
			normalized := strings.ToLower(strings.TrimSpace(name))
			if normalized == "" || len([]rune(name)) > 200 || (names[normalized] != "" && names[normalized] != standard.Key) {
				return fmt.Errorf("standard names and aliases must identify one standard")
			}
			names[normalized] = standard.Key
		}
	}
	return nil
}
