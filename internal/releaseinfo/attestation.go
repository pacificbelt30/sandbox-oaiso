package releaseinfo

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// VerifyAttestation delegates cryptography, transparency evidence and trusted
// Sigstore roots to the official GitHub CLI. Downloaded keys are never trusted.
// Every identity/ref constraint is fixed by this application, not artifact input.
func VerifyAttestation(ctx context.Context, artifact, bundle, tag string) error {
	if !ValidTag(tag) {
		return fmt.Errorf("invalid signed release tag")
	}
	executable, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("signed release verification requires GitHub CLI (gh); follow https://github.com/cli/cli/blob/trunk/docs/install_linux.md")
	}
	return verifyAttestation(ctx, executable, artifact, bundle, tag)
}
func verifyAttestation(ctx context.Context, executable, artifact, bundle, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	publisher := Repository
	// These published releases were signed before the repository rename.
	switch tag {
	case "v0.0.4", "v0.0.5", "v0.0.6", "v0.0.7", "v0.0.8":
		publisher = "pacificbelt30/sandbox-oasis"
	}
	// GitHub CLI permits one signer selector. The exact certificate identity
	// already binds both the workflow path and tag; do not add signer-workflow.
	command := exec.CommandContext(ctx, executable, "attestation", "verify", artifact, "--bundle", bundle, "--repo", publisher, "--source-ref", "refs/tags/"+tag, "--cert-identity", "https://github.com/"+publisher+"/.github/workflows/release.yml@refs/tags/"+tag, "--cert-oidc-issuer", "https://token.actions.githubusercontent.com", "--deny-self-hosted-runners", "--hostname", "github.com")
	if err := command.Run(); err != nil {
		return fmt.Errorf("release signature/provenance verification failed; refusing installation")
	}
	return nil
}
