# Terraform Provider Upgrade Automation Design

## Goals

- Detect changes introduced by bumping `terraform-provider-oci` (and Terraform core) versions.
- Categorise CRD churn into **added**, **removed**, **modified**, and **renamed** resources.
- Highlight schema-level risk (e.g. removed properties, new required fields) versus non-breaking additions.
- Correlate high-risk items with upstream CHANGELOG notes.
- Produce machine-readable and human-friendly reports to drive manual validation and automated testing.

## Components

| Component | Location | Responsibility |
| --- | --- | --- |
| `make generate` wrapper | `hack/terraform-upgrade.sh` (TBD) | Run generation for specific provider/Terraform versions and snapshot outputs. |
| CRD audit CLI | `cmd/crd-audit` | Compare two CRD directories, classify file-level diffs, analyse schemas, and emit reports. |
| Report schema | `internal/upgradeaudit` | Common structs for JSON output plus helpers for Markdown rendering. |
| Release-note correlator | `cmd/crd-audit` | Parse changelog snippets and tag expected breaking changes. |
| CI glue | `.github/workflows/terraform-upgrade.yaml` (later) | Enforce clean audits on PRs and surface artifacts. |

## Data Flow

1. **Snapshot generation**
   - `hack/terraform-upgrade.sh --provider 7.27.0 --out .work/crds-7.27.0`
   - `hack/terraform-upgrade.sh --provider 8.9.0 --out .work/crds-8.9.0`
2. **Audit run**
   - `go run ./cmd/crd-audit --old .work/crds-7.27.0 --new .work/crds-8.9.0 --report reports/terraform-upgrade/7.27.0-8.9.0`
3. **Outputs**
   - `summary.json`: structured list of services, resources, risk level, notes.
   - `summary.md`: formatted report for reviewers.
   - `raw-diff.txt`: optional copy of `diff -qr`.
4. **Review**
   - Engineers acknowledge breaking changes, update examples/configs, then re-run audit.
5. **Testing**
   - CI/Argo pipeline consumes summary to focus regression suites.

## Change Classification Rules

| Signal | Classification |
| --- | --- |
| New resource (added file) | Non-breaking (unless rename detection links to removed resource). |
| Removed resource | Breaking (unless paired with confirmed rename). |
| Schema property removed / type narrowed | Breaking. |
| Optional → required field | Breaking. |
| Required → optional | Non-breaking but noteworthy. |
| Enum shrinkage / default change | Potentially breaking; flag for review. |
| New optional property | Non-breaking. |
| Deprecation note | Informational; include in report. |

## Implementation Phases

1. **Scaffolding**
   - Create CLI, file walkers, baseline diff (added/removed/modified).
   - Emit JSON summary without schema grading (placeholder `unknown` risk).
2. **Schema engine**
   - Parse CRDs (multi-document YAML) into `apiextensionsv1.CustomResourceDefinition`.
   - Compare `OpenAPIV3Schema` objects per version using structural diffs.
   - Populate risk classification table.
3. **Rename heuristics & release notes**
   - Identify renamed resources by comparing signature hashes.
   - Fetch CHANGELOG segments (local checkout or GitHub raw) and tag matches.
4. **Reporting polish**
   - Render Markdown/HTML.
   - Provide CLI flags for output directories and verbosity.
5. **CI integration**
   - Add GitHub Action (or equivalent) to run audit on PRs touching provider versions.
   - Fail builds on unacknowledged breaking items; upload reports as artifacts.

## Open Questions

- Where should acknowledgement files live? Proposal: `upgrade-notes/<from>-<to>.yaml`.
- Should we snapshot Terraform docs or other generated artifacts alongside CRDs?
- Do we need to diff controller source (`internal/controller/...`) as part of the same pipeline?

## Quick Commands

- Capture CRD diff list:  
  `diff -qr /tmp/crds-v7.27.0 package/crds > /tmp/crd-diff.txt`

- Schema diff (single resource):  
  `yq -o=json '.spec.versions[].schema.openAPIV3Schema' old.yaml > old.json`  
  `yq -o=json '.spec.versions[].schema.openAPIV3Schema' new.yaml > new.json`  
  `diff -u old.json new.json`

- Full report (JSON):  
  `go run ./cmd/crd-audit --old .work/crds-7.27.0 --new .work/crds-8.9.0 --out reports/terraform-upgrade/7.27.0-8.9.0.json`

- Service delta summary:  
  `GOCACHE=/tmp/go-build go run ./cmd/crd-delta --old /tmp/crds-v7.27.0 --new package`
