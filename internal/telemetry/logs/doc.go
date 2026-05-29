// Package logs provides legacy node-agent log collectors and bootstrap
// formatter compatibility.
//
// New product-specific SIEM parser work belongs in internal/contentpacks with
// replayed samples and coverage state evidence. Formatter files in this package
// should stay limited to compatibility behavior and bug fixes while content
// packs become the durable parser/source authority.
package logs
