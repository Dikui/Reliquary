package types

import "time"

type ShowcaseShelf struct {
	Library      ShowcaseNamedEntity   `json:"library"`
	AsOf         time.Time             `json:"as_of"`
	Regions      []ShowcaseRegion      `json:"regions"`
	ProductLines []ShowcaseNamedEntity `json:"product_lines"`
	Models       []ShowcaseModel       `json:"models"`
}

type ShowcaseNamedEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ShowcaseRegion struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type ShowcaseModel struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	ProductName    string                  `json:"product_name"`
	Platform       string                  `json:"platform"`
	ProductLine    ShowcaseNamedEntity     `json:"product_line"`
	CurrentStage   string                  `json:"current_stage"`
	SaleStatuses   []ShowcaseSaleStatus    `json:"sale_statuses"`
	Checklist      []ShowcaseChecklistItem `json:"checklist"`
	Certifications []ShowcaseCertification `json:"certifications"`
}

type ShowcaseSaleStatus struct {
	Region      string `json:"region"`
	RegionName  string `json:"region_name"`
	Status      string `json:"status"`
	Raw         string `json:"raw"`
	Notes       string `json:"notes"`
	PlannedEOM  bool   `json:"planned_eom"`
	Conditional bool   `json:"conditional"`
}

type ShowcaseChecklistItem struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	StatusLabel string `json:"status_label"`
	Gap         bool   `json:"gap"`
	HasFile     bool   `json:"has_file"`
}

type ShowcaseCertification struct {
	StandardName string `json:"standard_name"`
	Status       string `json:"status"`
	RegionName   string `json:"region_name"`
	Origin       string `json:"origin"`
}
