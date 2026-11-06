package signature_test

import (
	"testing"
	"time"

	"github.com/shopware/sdk-go/signature"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyRegistrationDual_NewShop_AppSigValid(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	rawQuery := "shop-id=s1&shop-url=https://shop.test&timestamp=1"
	appSig := signature.ComputeSignature([]byte(rawQuery), "app-secret")

	err := dv.VerifyRegistrationDual(appSig, "", rawQuery, "app-secret", nil, false)
	require.NoError(t, err)
}

func TestVerifyRegistrationDual_NewShop_AppSigInvalid(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	err := dv.VerifyRegistrationDual("bad-sig", "", "shop-id=s1", "app-secret", nil, false)
	require.Error(t, err)
}

// Re-registering a confirmed shop requires proof of its current secret. The app secret
// alone is not proof: it ships in the manifest and identifies the app, not the shop.
func TestVerifyRegistrationDual_ReRegistration_RequiresShopSig(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	rawQuery := "shop-id=s1&shop-url=https://shop.test&timestamp=1"
	appSig := signature.ComputeSignature([]byte(rawQuery), "app-secret")
	shopSig := signature.ComputeSignature([]byte(rawQuery), "shop-secret")

	newShopState := func() *signature.ShopState {
		return &signature.ShopState{
			ShopSecret:            "shop-secret",
			RegistrationConfirmed: true,
		}
	}

	t.Run("valid shop signature accepted", func(t *testing.T) {
		err := dv.VerifyRegistrationDual(appSig, shopSig, rawQuery, "app-secret", newShopState(), false)
		require.NoError(t, err)
	})

	t.Run("absent shop signature rejected", func(t *testing.T) {
		err := dv.VerifyRegistrationDual(appSig, "", rawQuery, "app-secret", newShopState(), false)
		require.ErrorIs(t, err, signature.ErrMissingShopSignature)
	})

	t.Run("invalid shop signature rejected", func(t *testing.T) {
		wrongSig := signature.ComputeSignature([]byte(rawQuery), "not-the-shop-secret")
		err := dv.VerifyRegistrationDual(appSig, wrongSig, rawQuery, "app-secret", newShopState(), false)
		require.Error(t, err)
		require.NotErrorIs(t, err, signature.ErrMissingShopSignature)
	})
}

// The escape hatch waives the requirement for legacy shops only, and still verifies a
// signature that does arrive — so an upgraded Shopware ratchets the shop back to strict.
func TestVerifyRegistrationDual_AllowUnsignedReregistration(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	rawQuery := "shop-id=s1&shop-url=https://shop.test&timestamp=1"
	appSig := signature.ComputeSignature([]byte(rawQuery), "app-secret")

	t.Run("legacy shop passes without a shop signature", func(t *testing.T) {
		shop := &signature.ShopState{
			ShopSecret:                     "shop-secret",
			RegistrationConfirmed:          true,
			HasVerifiedWithDoubleSignature: false,
		}

		err := dv.VerifyRegistrationDual(appSig, "", rawQuery, "app-secret", shop, true)
		require.NoError(t, err)
	})

	t.Run("a supplied signature is still verified", func(t *testing.T) {
		shop := &signature.ShopState{
			ShopSecret:                     "shop-secret",
			RegistrationConfirmed:          true,
			HasVerifiedWithDoubleSignature: false,
		}
		wrongSig := signature.ComputeSignature([]byte(rawQuery), "not-the-shop-secret")

		err := dv.VerifyRegistrationDual(appSig, wrongSig, rawQuery, "app-secret", shop, true)
		require.Error(t, err)
	})

	t.Run("ratcheted shop cannot be downgraded by the hatch", func(t *testing.T) {
		shop := &signature.ShopState{
			ShopSecret:                     "shop-secret",
			RegistrationConfirmed:          true,
			HasVerifiedWithDoubleSignature: true,
		}

		err := dv.VerifyRegistrationDual(appSig, "", rawQuery, "app-secret", shop, true)
		require.ErrorIs(t, err, signature.ErrMissingShopSignature)
	})
}

func TestVerifyConfirmationDual_FirstRegistration(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"shopId":"s1"}`)
	shop := &signature.ShopState{
		PendingShopSecret:     "pending-secret",
		RegistrationConfirmed: false,
	}

	sig := signature.ComputeSignature(body, "pending-secret")

	err := dv.VerifyConfirmationDual(sig, "", body, shop, false)
	require.NoError(t, err)
}

func TestVerifyConfirmationDual_ReRegistration_DualSig(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"shopId":"s1"}`)
	shop := &signature.ShopState{
		ShopSecret:            "old-secret",
		PendingShopSecret:     "new-secret",
		RegistrationConfirmed: true,
	}

	newSig := signature.ComputeSignature(body, "new-secret")
	prevSig := signature.ComputeSignature(body, "old-secret")

	err := dv.VerifyConfirmationDual(newSig, prevSig, body, shop, false)
	require.NoError(t, err)
}

// Knowing the pending secret is not enough to confirm a rotation: the app handed that
// secret out itself, so it proves nothing about who is asking.
func TestVerifyConfirmationDual_ReRegistration_MissingPreviousSig(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"shopId":"s1"}`)
	newShopState := func() *signature.ShopState {
		return &signature.ShopState{
			ShopSecret:            "old-secret",
			PendingShopSecret:     "new-secret",
			RegistrationConfirmed: true,
		}
	}

	newSig := signature.ComputeSignature(body, "new-secret")

	t.Run("rejected by default", func(t *testing.T) {
		err := dv.VerifyConfirmationDual(newSig, "", body, newShopState(), false)
		require.ErrorIs(t, err, signature.ErrMissingPreviousSignature)
	})

	t.Run("invalid previous signature rejected", func(t *testing.T) {
		wrongPrev := signature.ComputeSignature(body, "not-the-old-secret")
		err := dv.VerifyConfirmationDual(newSig, wrongPrev, body, newShopState(), false)
		require.Error(t, err)
		require.NotErrorIs(t, err, signature.ErrMissingPreviousSignature)
	})

	t.Run("waived for a legacy shop under the escape hatch", func(t *testing.T) {
		err := dv.VerifyConfirmationDual(newSig, "", body, newShopState(), true)
		require.NoError(t, err)
	})

	t.Run("still required for a ratcheted shop under the escape hatch", func(t *testing.T) {
		shop := newShopState()
		shop.HasVerifiedWithDoubleSignature = true

		err := dv.VerifyConfirmationDual(newSig, "", body, shop, true)
		require.ErrorIs(t, err, signature.ErrMissingPreviousSignature)
	})
}

func TestVerifyWebhookWithFallback_CurrentSecret(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"event":"order.placed"}`)
	shop := &signature.ShopState{
		ShopSecret: "current-secret",
	}

	sig := signature.ComputeSignature(body, "current-secret")

	err := dv.VerifyWebhookWithFallback(sig, body, shop)
	require.NoError(t, err)
}

func TestVerifyWebhookWithFallback_PreviousSecret_WithinWindow(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"event":"order.placed"}`)
	rotatedAt := time.Now().Add(-30 * time.Second)
	shop := &signature.ShopState{
		ShopSecret:         "new-secret",
		PreviousShopSecret: "old-secret",
		SecretsRotatedAt:   &rotatedAt,
	}

	sig := signature.ComputeSignature(body, "old-secret")

	err := dv.VerifyWebhookWithFallback(sig, body, shop)
	require.NoError(t, err)
}

func TestVerifyWebhookWithFallback_PreviousSecret_OutsideWindow(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"event":"order.placed"}`)
	rotatedAt := time.Now().Add(-120 * time.Second)
	shop := &signature.ShopState{
		ShopSecret:         "new-secret",
		PreviousShopSecret: "old-secret",
		SecretsRotatedAt:   &rotatedAt,
	}

	sig := signature.ComputeSignature(body, "old-secret")

	err := dv.VerifyWebhookWithFallback(sig, body, shop)
	require.Error(t, err)
}

func TestVerifyWebhookWithFallback_InvalidSignature(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"event":"order.placed"}`)
	shop := &signature.ShopState{
		ShopSecret: "current-secret",
	}

	err := dv.VerifyWebhookWithFallback("bad-sig", body, shop)
	require.Error(t, err)
}

func TestVerifyConfirmationDual_FirstRegistration_NoPreviousSigRequired(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"shopId":"s1"}`)
	shop := &signature.ShopState{
		PendingShopSecret:     "secret",
		RegistrationConfirmed: false,
	}

	sig := signature.ComputeSignature(body, "secret")

	err := dv.VerifyConfirmationDual(sig, "", body, shop, false)
	require.NoError(t, err)
}

func TestVerifyConfirmationDual_MissingPendingSecret(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"shopId":"s1"}`)
	shop := &signature.ShopState{
		ShopSecret:            "secret",
		RegistrationConfirmed: true,
	}

	sig := signature.ComputeSignature(body, "secret")

	err := dv.VerifyConfirmationDual(sig, "", body, shop, false)
	require.Error(t, err)
	require.ErrorIs(t, err, signature.ErrMissingPendingShopSecret)
	assert.Contains(t, err.Error(), "pending shop secret")
}

func TestVerifyConfirmationDual_MissingShopState(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"shopId":"s1"}`)
	sig := signature.ComputeSignature(body, "secret")

	err := dv.VerifyConfirmationDual(sig, "", body, nil, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, signature.ErrMissingPendingShopSecret)
}

func TestVerifyConfirmationDual_InvalidPrimarySignature(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`{"shopId":"s1"}`)
	shop := &signature.ShopState{
		ShopSecret:            "old-secret",
		PendingShopSecret:     "pending-secret",
		RegistrationConfirmed: true,
	}

	err := dv.VerifyConfirmationDual("bad-signature", "", body, shop, false)
	require.Error(t, err)
}

func TestVerifyWebhookWithFallback_CustomAllowance_WithinExtendedWindow(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 2*time.Minute)

	body := []byte(`{"event":"order.placed"}`)
	rotatedAt := time.Now().Add(-90 * time.Second)
	shop := &signature.ShopState{
		ShopSecret:         "new-secret",
		PreviousShopSecret: "old-secret",
		SecretsRotatedAt:   &rotatedAt,
	}

	sig := signature.ComputeSignature(body, "old-secret")

	err := dv.VerifyWebhookWithFallback(sig, body, shop)
	require.NoError(t, err, "90s elapsed should be within a 120s custom allowance")
}

func TestVerifyWebhookWithFallback_CustomAllowance_OutsideExtendedWindow(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 2*time.Minute)

	body := []byte(`{"event":"order.placed"}`)
	rotatedAt := time.Now().Add(-150 * time.Second)
	shop := &signature.ShopState{
		ShopSecret:         "new-secret",
		PreviousShopSecret: "old-secret",
		SecretsRotatedAt:   &rotatedAt,
	}

	sig := signature.ComputeSignature(body, "old-secret")

	err := dv.VerifyWebhookWithFallback(sig, body, shop)
	require.Error(t, err, "150s elapsed should exceed a 120s custom allowance")
}

func TestVerifyWebhookWithFallback_NoPreviousSecret(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	body := []byte(`test`)
	shop := &signature.ShopState{
		ShopSecret: "secret",
	}

	err := dv.VerifyWebhookWithFallback("bad-sig", body, shop)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
}

// TestSSRF_AttackerReRegistersWithPublicAppSecret models the core attack (SEC-AUTH-
// REGISTRATION-L51 / GHSA-c4p7-rwrg-pf6p): an attacker who knows the app secret from
// manifest.xml re-registers a victim shop to redirect its traffic and steal its secret.
//
// The victim is deliberately a legacy shop with HasVerifiedWithDoubleSignature false,
// because that is the only state the attack ever worked in. Asserting against a shop
// that already ratcheted proves nothing — it was never reachable.
func TestSSRF_AttackerReRegistersWithPublicAppSecret(t *testing.T) {
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	publicAppSecret := "dummy-secret-from-manifest"
	realShopSecret := "real-shop-secret-attacker-does-not-know"

	legacyShop := func() *signature.ShopState {
		return &signature.ShopState{
			RegistrationConfirmed:          true,
			ShopSecret:                     realShopSecret,
			HasVerifiedWithDoubleSignature: false,
		}
	}

	attackerQuery := "shop-id=victim-shop&shop-url=https://attacker.evil/callback&timestamp=9999999999"
	validAppSig := signature.ComputeSignature([]byte(attackerQuery), publicAppSecret)

	t.Run("phase 1: omitting the shop signature no longer disables the check", func(t *testing.T) {
		err := dv.VerifyRegistrationDual(validAppSig, "", attackerQuery, publicAppSecret, legacyShop(), false)
		require.ErrorIs(t, err, signature.ErrMissingShopSignature)
	})

	t.Run("phase 1: forged shop signature rejected", func(t *testing.T) {
		fakeShopSig := signature.ComputeSignature([]byte(attackerQuery), "guessed-wrong-secret")

		err := dv.VerifyRegistrationDual(validAppSig, fakeShopSig, attackerQuery, publicAppSecret, legacyShop(), false)
		require.Error(t, err)
	})

	t.Run("phase 2: the pending secret alone cannot confirm a rotation", func(t *testing.T) {
		confirmBody := []byte(`{"shopId":"victim-shop","apiKey":"ak","secretKey":"sk"}`)

		shopAfterRegistration := legacyShop()
		shopAfterRegistration.PendingShopSecret = "new-pending-secret"

		// The attacker holds the pending secret — the app handed it out — so this signature
		// is genuinely valid. Only the previous-secret proof stops the promotion.
		validPendingSig := signature.ComputeSignature(confirmBody, "new-pending-secret")

		err := dv.VerifyConfirmationDual(validPendingSig, "", confirmBody, shopAfterRegistration, false)
		require.ErrorIs(t, err, signature.ErrMissingPreviousSignature)
	})

	t.Run("a ratcheted shop stays protected even under the escape hatch", func(t *testing.T) {
		shop := legacyShop()
		shop.HasVerifiedWithDoubleSignature = true

		err := dv.VerifyRegistrationDual(validAppSig, "", attackerQuery, publicAppSecret, shop, true)
		require.ErrorIs(t, err, signature.ErrMissingShopSignature)
	})
}
