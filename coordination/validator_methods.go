// coordination/validator_methods.go
package coordination

import (
	"context"
	"errors"
)

// GetParameterValidator returns the parameter validator for this coordinator
func (c *Coordinator) GetParameterValidator() (*ParameterValidator, error) {
	return c.getOrCreateValidator()
}

// InitializeParameterValidation initializes the parameter validation system
func (c *Coordinator) InitializeParameterValidation(config *ParameterValidatorConfig) error {
	validator, err := c.getOrCreateValidator()
	if err != nil {
		return err
	}

	c.logf("Parameter validation initialized with batch size %d", validator.maxBatchSize)
	return nil
}

// ValidateParameterSync provides a synchronous parameter validation method
func (c *Coordinator) ValidateParameterSync(ctx context.Context, data []byte) ([]byte, string, error) {
	validator, err := c.GetParameterValidator()
	if err != nil {
		return nil, "", err
	}

	result, err := validator.validateParameterImmediate(ctx, data)
	if err != nil {
		return nil, "", err
	}

	if !result.Success {
		if result.Error != nil {
			return nil, "", result.Error
		}
		return nil, "", errors.New("parameter validation failed")
	}

	return result.Data, result.Format, nil
}
