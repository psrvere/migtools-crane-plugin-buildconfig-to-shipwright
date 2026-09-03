package buildconfig

import "fmt"

// OutcomeState is the disposition of a single BuildConfig conversion. Every
// BuildConfig ends in exactly one state (BUILD-2318).
type OutcomeState string

const (
	// OutcomeConverted: a Shipwright Build was generated with no warnings.
	OutcomeConverted OutcomeState = "converted"
	// OutcomeConvertedWithWarnings: a Build was generated, but something was
	// dropped or needs review — the warnings are on the Outcome and in the logs.
	OutcomeConvertedWithWarnings OutcomeState = "converted-with-warnings"
	// OutcomeSkipped: the BuildConfig was intentionally not converted (e.g. an
	// unsupported strategy or a missing output image). It is passed through
	// unchanged.
	OutcomeSkipped OutcomeState = "skipped"
	// OutcomeFailed: conversion hit an error. The BuildConfig is passed through
	// unchanged so the rest of the migration can continue (crane aborts the
	// whole run on any plugin error, so the plugin never returns one for a
	// single-BuildConfig failure).
	OutcomeFailed OutcomeState = "failed"
)

// Outcome describes how one BuildConfig conversion ended.
type Outcome struct {
	State  OutcomeState
	Reason string // why it was skipped or failed; empty when converted
	// Warnings holds every conversion warning recorded while producing this
	// Build, so a converted-with-warnings Outcome is self-describing and a
	// caller (e.g. the BUILD-2319 report) need not re-parse logs. Empty for a
	// clean conversion; not populated for skipped/failed outcomes.
	Warnings []string
}

// outcomeConverted is a convenience for a successful conversion; State is set to
// converted-with-warnings later if any warning was recorded.
func outcomeConverted() Outcome            { return Outcome{State: OutcomeConverted} }
func outcomeSkipped(reason string) Outcome { return Outcome{State: OutcomeSkipped, Reason: reason} }
func outcomeFailed(reason string) Outcome  { return Outcome{State: OutcomeFailed, Reason: reason} }

// warnf is the one way to record a conversion warning: it prefixes the message
// with the BuildConfig it came from, appends it to c.warnings, and logs it at
// WARN. c.warnings is the single source that drives converted-with-warnings,
// Outcome.Warnings, and the ConversionWarningsAnnotation that Convert writes.
//
// All field-drop and degraded-conversion messages must go through warnf. A
// message logged directly via c.Log would not be counted, so the BuildConfig
// would be reported as cleanly converted while the field was silently dropped
// (BUILD-2319). The one deliberate exception is the inline Dockerfile on a
// Docker strategy, which records into c.warnings explicitly alongside a louder
// ERROR.
func (c *Converter) warnf(format string, args ...interface{}) {
	c.Log.Warn(c.recordWarning(fmt.Sprintf(format, args...)))
}

// recordWarning attributes a message to the BuildConfig being converted, appends
// it to c.warnings, and returns the attributed text for the caller to log.
//
// It exists so the one call site that logs a drop at ERROR rather than WARN (the
// inline Dockerfile on a Docker strategy) still gets the same attribution as
// warnf, instead of being the only entry in the annotation with no BuildConfig
// on it.
func (c *Converter) recordWarning(msg string) string {
	// curName is empty only for a warning raised outside Convert; such a message
	// has no BuildConfig to attribute and is recorded unprefixed.
	if c.curName != "" {
		msg = fmt.Sprintf("[%s/%s] %s", c.curNS, c.curName, msg)
	}
	c.warnings = append(c.warnings, msg)
	return msg
}
