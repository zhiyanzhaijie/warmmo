package persistence

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"warmmo/core/internal/domain/ai"
)

const masterKeySize = 32

var (
	ErrAPIKeyRequired                = ai.ErrAPIKeyRequired
	ErrProviderConfigurationNotFound = ai.ErrProviderConfigurationNotFound
)

type ProviderRepository struct {
	database     *gorm.DB
	databaseHost *Database
	ownsDatabase bool
	keyPath      string
}

type encryptedSecret struct {
	Nonce      []byte
	Ciphertext []byte
}

func NewProviderRepository(dataDirectory string) (*ProviderRepository, error) {
	database, err := OpenDatabase(dataDirectory)
	if err != nil {
		return nil, err
	}
	repository := NewProviderRepositoryWithDatabase(database)
	repository.ownsDatabase = true
	return repository, nil
}

func NewProviderRepositoryWithDatabase(database *Database) *ProviderRepository {
	return &ProviderRepository{
		database:     database.DB,
		databaseHost: database,
		keyPath:      filepath.Join(database.DataDirectory(), ".master-key"),
	}
}

func (r *ProviderRepository) Close() error {
	if !r.ownsDatabase {
		return nil
	}
	return r.databaseHost.Close()
}

func (r *ProviderRepository) DatabasePath() string { return r.databaseHost.Path() }

func (r *ProviderRepository) List() ([]ai.ProviderConfiguration, error) {
	var models []providerConfigurationModel
	if err := r.database.Order("updated_at DESC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("query provider configurations: %w", err)
	}
	configurations := make([]ai.ProviderConfiguration, len(models))
	for index, model := range models {
		configurations[index] = providerConfigurationFromModel(model)
	}
	return configurations, nil
}

func (r *ProviderRepository) Save(input ai.SaveProviderConfiguration) (ai.ProviderConfiguration, error) {
	var saved providerConfigurationModel
	err := r.database.Transaction(func(tx *gorm.DB) error {
		var existing providerConfigurationModel
		err := tx.Select("secret_nonce", "secret_ciphertext", "api_key_hint").
			Where("provider_id = ?", input.ProviderID).
			First(&existing).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query existing provider configuration: %w", err)
		}

		secret := encryptedSecret{Nonce: existing.SecretNonce, Ciphertext: existing.SecretCiphertext}
		keyHint := existing.APIKeyHint
		if input.APIKey != "" {
			secret, err = r.encrypt(input.APIKey)
			if err != nil {
				return err
			}
			keyHint = apiKeyHint(input.APIKey)
		} else if !exists {
			return ErrAPIKeyRequired
		}

		saved = providerConfigurationModel{
			ID: input.ProviderID, ProviderID: input.ProviderID, BaseURL: input.BaseURL,
			ModelIDs:    append([]string(nil), input.ModelIDs...),
			SecretNonce: secret.Nonce, SecretCiphertext: secret.Ciphertext, APIKeyHint: keyHint,
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "provider_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"base_url", "model_ids_json", "secret_nonce", "secret_ciphertext", "api_key_hint", "updated_at"}),
		}).Create(&saved).Error
	})
	if err != nil {
		return ai.ProviderConfiguration{}, err
	}
	return providerConfigurationFromModel(saved), nil
}

func (r *ProviderRepository) Delete(providerID string) error {
	result := r.database.Where("provider_id = ?", providerID).Delete(&providerConfigurationModel{})
	if result.Error != nil {
		return fmt.Errorf("delete provider configuration: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrProviderConfigurationNotFound
	}
	return nil
}

func (r *ProviderRepository) GetAPIKey(providerID string) (string, error) {
	var model providerConfigurationModel
	err := r.database.Select("secret_nonce", "secret_ciphertext").
		Where("provider_id = ?", providerID).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrProviderConfigurationNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query provider secret: %w", err)
	}
	return r.decrypt(encryptedSecret{Nonce: model.SecretNonce, Ciphertext: model.SecretCiphertext})
}

func (r *ProviderRepository) ResolveModel(providerID, modelID string) (string, string, error) {
	var model providerConfigurationModel
	err := r.database.Select("base_url", "model_ids_json").
		Where("provider_id = ?", providerID).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", ErrProviderConfigurationNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("query provider model: %w", err)
	}
	for _, enabledModelID := range model.ModelIDs {
		if enabledModelID == modelID {
			apiKey, err := r.GetAPIKey(providerID)
			return model.BaseURL, apiKey, err
		}
	}
	return "", "", fmt.Errorf("model %q is not enabled for provider %q", modelID, providerID)
}

func providerConfigurationFromModel(model providerConfigurationModel) ai.ProviderConfiguration {
	return ai.ProviderConfiguration{
		ID: model.ID, ProviderID: model.ProviderID, BaseURL: model.BaseURL,
		ModelIDs:         append([]string(nil), model.ModelIDs...),
		APIKeyConfigured: len(model.SecretCiphertext) > 0,
		APIKeyHint:       model.APIKeyHint, UpdatedAt: model.UpdatedAt,
	}
}

func (r *ProviderRepository) encrypt(plaintext string) (encryptedSecret, error) {
	key, err := r.loadOrCreateMasterKey()
	if err != nil {
		return encryptedSecret{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return encryptedSecret{}, fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return encryptedSecret{}, fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return encryptedSecret{}, fmt.Errorf("create secret nonce: %w", err)
	}
	return encryptedSecret{Nonce: nonce, Ciphertext: gcm.Seal(nil, nonce, []byte(plaintext), nil)}, nil
}

func (r *ProviderRepository) decrypt(secret encryptedSecret) (string, error) {
	key, err := r.loadOrCreateMasterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, secret.Nonce, secret.Ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt provider secret: %w", err)
	}
	return string(plaintext), nil
}

func (r *ProviderRepository) loadOrCreateMasterKey() ([]byte, error) {
	key, err := os.ReadFile(r.keyPath)
	if err == nil {
		if len(key) != masterKeySize {
			return nil, errors.New("invalid master key length")
		}
		if err := os.Chmod(r.keyPath, 0o600); err != nil {
			return nil, fmt.Errorf("secure master key: %w", err)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	key = make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	file, err := os.OpenFile(r.keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return r.loadOrCreateMasterKey()
	}
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write master key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close master key: %w", err)
	}
	return key, nil
}

func apiKeyHint(apiKey string) string {
	if len(apiKey) <= 4 {
		return "••••"
	}
	return "••••" + apiKey[len(apiKey)-4:]
}
