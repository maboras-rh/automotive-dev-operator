package buildapi

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	apiserverv1beta1 "k8s.io/apiserver/pkg/apis/apiserver/v1beta1"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	tokenunion "k8s.io/apiserver/pkg/authentication/token/union"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	oidcauth "k8s.io/apiserver/plugin/pkg/authenticator/token/oidc"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	automotivev1alpha1 "github.com/centos-automotive-suite/automotive-dev-operator/api/v1alpha1"
	apiserver "k8s.io/apiserver/pkg/apis/apiserver"
)

// AuthenticationConfiguration defines the authentication configuration structure.
type AuthenticationConfiguration struct {
	ClientID string                              `json:"clientId"`
	Internal InternalAuthConfig                  `json:"internal"`
	JWT      []apiserverv1beta1.JWTAuthenticator `json:"jwt"`
}

// InternalAuthConfig defines internal authentication configuration.
type InternalAuthConfig struct {
	Prefix string `json:"prefix"`
}

// jwtConfigsEqual compares two JWT authenticator slices to check if they're effectively equal.
// This is used to determine if we need to recreate the OIDC authenticator.
func jwtConfigsEqual(a, b []apiserverv1beta1.JWTAuthenticator) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// Compare key fields that would affect authentication
		if a[i].Issuer.URL != b[i].Issuer.URL {
			return false
		}
		if a[i].Issuer.CertificateAuthority != b[i].Issuer.CertificateAuthority {
			return false
		}
		if !reflect.DeepEqual(a[i].Issuer.Audiences, b[i].Issuer.Audiences) {
			return false
		}
		if a[i].ClaimMappings.Username.Claim != b[i].ClaimMappings.Username.Claim {
			return false
		}
		if a[i].ClaimMappings.Groups.Claim != b[i].ClaimMappings.Groups.Claim {
			return false
		}
		// Compare prefixes
		aUsernamePrefix := ""
		bUsernamePrefix := ""
		if a[i].ClaimMappings.Username.Prefix != nil {
			aUsernamePrefix = *a[i].ClaimMappings.Username.Prefix
		}
		if b[i].ClaimMappings.Username.Prefix != nil {
			bUsernamePrefix = *b[i].ClaimMappings.Username.Prefix
		}
		if aUsernamePrefix != bUsernamePrefix {
			return false
		}
	}
	return true
}

// authConfigsEqual compares two authentication configurations to check if they're effectively equal.
func authConfigsEqual(a, b *AuthenticationConfiguration) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.ClientID != b.ClientID {
		return false
	}
	if a.Internal.Prefix != b.Internal.Prefix {
		return false
	}
	return jwtConfigsEqual(a.JWT, b.JWT)
}

func newJWTAuthenticator(ctx context.Context, config AuthenticationConfiguration) (authenticator.Token, error) {
	logger := log.FromContext(ctx)
	if len(config.JWT) == 0 {
		logger.Info("No JWT issuers configured")
		return nil, nil
	}

	scheme := runtime.NewScheme()
	_ = apiserver.AddToScheme(scheme)
	_ = apiserverv1beta1.AddToScheme(scheme)

	jwtAuthenticators := make([]authenticator.Token, 0, len(config.JWT))
	for _, jwtAuthenticator := range config.JWT {
		issuerURL := jwtAuthenticator.Issuer.URL
		hasCustomCA := jwtAuthenticator.Issuer.CertificateAuthority != ""

		var oidcCAContent oidcauth.CAContentProvider
		if hasCustomCA {
			var oidcCAError error
			// Try to read CA from file, or use it as inline PEM
			if _, err := os.Stat(jwtAuthenticator.Issuer.CertificateAuthority); err == nil {
				oidcCAContent, oidcCAError = dynamiccertificates.NewDynamicCAContentFromFile(
					"oidc-authenticator",
					jwtAuthenticator.Issuer.CertificateAuthority,
				)
				jwtAuthenticator.Issuer.CertificateAuthority = ""
			} else {
				oidcCAContent, oidcCAError = dynamiccertificates.NewStaticCAContent(
					"oidc-authenticator",
					[]byte(jwtAuthenticator.Issuer.CertificateAuthority),
				)
			}
			if oidcCAError != nil {
				logger.Error(oidcCAError, "Failed to load CA certificate", "issuer", issuerURL)
				return nil, oidcCAError
			}
		}

		var jwtAuthenticatorUnversioned apiserver.JWTAuthenticator
		if err := scheme.Convert(&jwtAuthenticator, &jwtAuthenticatorUnversioned, nil); err != nil {
			logger.Error(err, "Failed to convert JWT authenticator config", "issuer", issuerURL)
			return nil, err
		}

		oidcAuth, err := oidcauth.New(ctx, oidcauth.Options{
			JWTAuthenticator:     jwtAuthenticatorUnversioned,
			CAContentProvider:    oidcCAContent,
			SupportedSigningAlgs: oidcauth.AllValidSigningAlgorithms(),
		})
		if err != nil {
			logger.Error(err, "Failed to create OIDC authenticator", "issuer", issuerURL)
			return nil, err
		}
		jwtAuthenticators = append(jwtAuthenticators, oidcAuth)
	}
	logger.Info("JWT authenticators configured", "count", len(jwtAuthenticators))
	return tokenunion.NewFailOnError(jwtAuthenticators...), nil
}

// loadAuthenticationConfigurationFromOperatorConfig loads authentication configuration directly from OperatorConfig CRD.
func loadAuthenticationConfigurationFromOperatorConfig(ctx context.Context, k8sClient client.Client, namespace string) (*AuthenticationConfiguration, authenticator.Token, string, error) {
	operatorConfig := &automotivev1alpha1.OperatorConfig{}
	key := types.NamespacedName{Name: "config", Namespace: namespace}

	if err := k8sClient.Get(ctx, key, operatorConfig); err != nil {
		return nil, nil, "", fmt.Errorf("failed to get OperatorConfig %s/%s: %w", namespace, "config", err)
	}

	// Check if authentication is configured
	if operatorConfig.Spec.BuildAPI == nil {
		return nil, nil, "", nil // No authentication configured
	}
	if operatorConfig.Spec.BuildAPI.Authentication == nil {
		return nil, nil, "", nil // No authentication configured
	}

	auth := operatorConfig.Spec.BuildAPI.Authentication

	// Deep copy JWT config to avoid modifying the original CRD data
	// and ensure Prefix pointers are properly set (k8s OIDC authenticator requires non-nil Prefix when Claim is set)
	jwtCopy := make([]apiserverv1beta1.JWTAuthenticator, len(auth.JWT))
	for i, jwt := range auth.JWT {
		jwtCopy[i] = jwt
		// Ensure Prefix is not nil when Claim is set (k8s OIDC requirement)
		if jwt.ClaimMappings.Username.Claim != "" && jwt.ClaimMappings.Username.Prefix == nil {
			emptyPrefix := ""
			jwtCopy[i].ClaimMappings.Username.Prefix = &emptyPrefix
		}
		if jwt.ClaimMappings.Groups.Claim != "" && jwt.ClaimMappings.Groups.Prefix == nil {
			emptyPrefix := ""
			jwtCopy[i].ClaimMappings.Groups.Prefix = &emptyPrefix
		}
	}

	config := &AuthenticationConfiguration{
		ClientID: auth.ClientID,
		Internal: InternalAuthConfig{
			Prefix: "internal:",
		},
		JWT: jwtCopy,
	}

	// Set internal prefix if provided
	if auth.Internal != nil && auth.Internal.Prefix != "" {
		config.Internal.Prefix = auth.Internal.Prefix
	}

	// Build authenticator from JWT configuration
	authn, err := newJWTAuthenticator(ctx, *config)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create JWT authenticator, will fall back to kubeconfig authentication", "namespace", namespace)
		// Return config with nil authenticator - this allows kubeconfig fallback to work
		return config, nil, config.Internal.Prefix, nil
	}

	return config, authn, config.Internal.Prefix, nil
}
