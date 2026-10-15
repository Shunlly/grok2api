package repository

import "time"

// AccountModelRoute 是账号所属 Provider 下的一条公开模型路由及其与该账号的绑定关系。
type AccountModelRoute struct {
	ID            uint64
	PublicID      string
	UpstreamModel string
	Capability    string
	Enabled       bool
	HasBindings   bool
	Bound         bool
}

// AccountModelSnapshot 汇总单个账号的上游模型能力与可路由公开模型，供后台诊断展示。
type AccountModelSnapshot struct {
	Capabilities  []string
	Routes        []AccountModelRoute
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	LastError     string
}
