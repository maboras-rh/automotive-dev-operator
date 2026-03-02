# Release Process

This document describes how to create releases for the Automotive Dev Operator.

## Release Strategy

We use **release branches** for stable release series:

- **Main branch (`main`)**: Latest development
- **Release branches (`release-X.Y.x`)**: Stable release series for patches
- **Tags (`vX.Y.Z`)**: Specific release versions

### Branch Lifecycle

```text
main (development)
├── release-0.1.x → v0.1.0, v0.1.1, v0.1.2...
├── release-0.2.x → v0.2.0, v0.2.1, v0.2.2...
└── release-1.0.x → v1.0.0, v1.0.1, v1.0.2...
```

## Creating a New Release Series

For a new **minor** or **major** release (e.g., 0.2.0, 1.0.0):

### 1. Create Release Branch

```bash
# Creates release-X.Y.x branch and tags vX.Y.Z
./hack/create-release-branch.sh 0.2.0
```

This script:
- Creates `release-0.2.x` branch from `main`
- Updates `VERSION` in Makefile to `0.2.0`
- Creates and pushes tag `v0.2.0`
- Triggers GitHub Actions to build and release

### 2. Update Main Branch

```bash
git checkout main
sed -i 's/VERSION ?= 0.2.0/VERSION ?= 0.3.0-dev/' Makefile
git commit -m "Bump main to 0.3.0-dev for next development cycle"
git push origin main
```

## Creating a Patch Release

For **patch** releases on existing release branches (e.g., 0.2.1, 0.2.2):

### 1. Apply Fixes to Release Branch

```bash
# Switch to release branch
git checkout release-0.2.x

# Cherry-pick fixes from main (or commit directly)
git cherry-pick <commit-hash>

# Or make fixes directly on the release branch
git commit -m "Fix critical bug in image builds"
```

### 2. Create Patch Release

```bash
# Creates patch release tag
./hack/create-patch-release.sh 0.2.1
```

This script:
- Updates `VERSION` in Makefile to `0.2.1`
- Creates and pushes tag `v0.2.1`
- Triggers GitHub Actions to build and release

## Release Artifacts

Each release includes the following artifacts:

### Container Images
- **Operator Image**: `quay.io/rh-sdv-cloud/automotive-dev-operator:v0.1.0`
  - Multi-arch support (linux/amd64, linux/arm64)
- **Bundle Image**: `quay.io/rh-sdv-cloud/automotive-dev-operator-bundle:v0.1.0`
  - Contains OLM bundle for OperatorHub

### CLI Binaries
- `caib-0.1.0-amd64` (Linux AMD64)
- `caib-0.1.0-arm64` (Linux ARM64)
- `caib-0.1.0-darwin` (macOS ARM64)

### Installation Assets
- `install-v0.1.0.yaml` (version-pinned installer manifest)

## Manual Release Process

If you prefer manual control or need to troubleshoot:

### 1. Prepare Release

```bash
# Use interactive script
./hack/prepare-release.sh

# Or manual commands
export VERSION=0.1.0
make prepare-release VERSION=$VERSION
```

### 2. Create and Push Tag

```bash
git tag -a v$VERSION -m "Release $VERSION"
git push origin v$VERSION
```

### 3. Submit to OperatorHub (community-operators-prod)

Manual commands to submit the operator to OperatorHub:

#### Step 1: Generate Bundle Structure

```bash
# Generate community-operators-prod directory structure
make community-operators-bundle VERSION=0.1.0

# Verify the structure
ls -la community-operators-prod/operators/automotive-dev-operator/
# Should show: 0.1.0/ and ci.yaml
```

#### Step 2: Fork and Clone Repository

```bash
# Fork the repository at: https://github.com/redhat-openshift-ecosystem/community-operators-prod
# Then clone your fork:
git clone https://github.com/YOUR_USERNAME/community-operators-prod.git
cd community-operators-prod
```

#### Step 3: Copy Bundle to Fork

```bash
# Copy the generated bundle structure
cp -r ../automotive-dev-operator/community-operators-prod/operators/automotive-dev-operator ./operators/

# Verify the copy
ls -la operators/automotive-dev-operator/
# Should show: 0.1.0/ and ci.yaml
```

#### Step 4: Create PR

```bash
# Create branch for the submission
git checkout -b automotive-dev-operator-0.1.0

# Add and commit the files
git add operators/automotive-dev-operator/
git commit -m "operator automotive-dev-operator (0.1.0)"

# Push the branch
git push origin automotive-dev-operator-0.1.0

# Create PR (use gh CLI or web interface)
gh pr create \
  --title "operator automotive-dev-operator (0.1.0)" \
  --body "New operator submission for automotive-dev-operator version 0.1.0"
```

#### Step 5: Monitor PR

- The community-operators CI will validate your bundle
- Address any validation issues by updating your PR
- Merge after approval from maintainers

#### Validation Check (Optional)

Before submitting, validate locally:

```bash
# Run the same validations as CI
make bundle-validate-operatorhub VERSION=0.1.0

# Check bundle structure
operator-sdk bundle validate ./bundle --select-optional name=operatorhubv2
operator-sdk bundle validate ./bundle --select-optional name=capabilities
operator-sdk bundle validate ./bundle --select-optional name=categories
```

## GitHub Actions Workflow

The release process is automated through GitHub Actions:

### Build Workflow (`.github/workflows/build.yml`)

**Triggers:**
- Push to `main` or `release-*` branches
- Push of `v*` tags

**On release tags (`v*`):**
1. **Multi-arch operator images** (AMD64, ARM64)
2. **Bundle image** (OLM bundle)
3. **CLI binaries** (Linux AMD64/ARM64, macOS ARM64)
4. **GitHub Release** with artifacts

For detailed workflow documentation, see [workflows.md](workflows.md).

## Versioning

We follow [Semantic Versioning](https://semver.org/):

- **MAJOR** version: Incompatible API changes
- **MINOR** version: New functionality, backward-compatible
- **PATCH** version: Bug fixes, backward-compatible

### Version Numbering Examples

```text
0.1.0  → First minor release
0.1.1  → Patch release (bug fixes)
0.2.0  → Second minor release (new features)
1.0.0  → First major release (stable API)
1.0.1  → Patch release on stable series
1.1.0  → Minor release with new features
2.0.0  → Major release (breaking changes)
```

### Pre-release Versions

For development builds:
- Use semantic version with pre-release identifier: `1.0.0-alpha.1`, `1.0.0-beta.2`, `1.0.0-rc.1`

## Development Workflow Integration

### Main Branch Development
```bash
# Main branch is always ahead of releases
main (0.3.0-dev) ← Active development
├── release-0.2.x (0.2.0, 0.2.1...) ← Current stable
└── release-0.1.x (0.1.0, 0.1.1...) ← Previous stable
```

### Feature Development
1. Create feature branch from `main`
2. Develop and test features
3. Create PR to `main`
4. Merge when ready

### Hotfix Development
1. Create hotfix branch from appropriate `release-X.Y.x`
2. Fix the issue
3. Create patch release
4. Cherry-pick fix to `main` if needed
