// Package luban provides a small Go API for building AI applications with
// Luban. Business modules provide configuration, model gateway adapters,
// functions, Tools, and Skills. The Runtime adapts them to Eino and executes
// an ordered Agent flow.
//
// The SDK is validated with offline fake models and is integrated into the
// admissions SchoolPicker stage through the host module's model gateway
// adapter. Full database-backed strategy pipeline execution remains a
// separate admissions integration check.
package luban
