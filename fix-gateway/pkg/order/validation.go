// See the file LICENSE for licensing terms.

package order

import (
	"errors"
	"fmt"
)

// ValidationError represents an order validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validate performs validation checks on an order
func (o *Order) Validate() []ValidationError {
	var errors []ValidationError
	
	// Required fields validation
	if o.Symbol == "" {
		errors = append(errors, ValidationError{
			Field:   "Symbol",
			Message: "Symbol is required",
		})
	}
	
	if o.ClientOrderID == "" {
		errors = append(errors, ValidationError{
			Field:   "ClientOrderID",
			Message: "ClientOrderID is required",
		})
	}
	
	if o.BrokerID == "" {
		errors = append(errors, ValidationError{
			Field:   "BrokerID",
			Message: "BrokerID is required",
		})
	}
	
	if o.Account == "" {
		errors = append(errors, ValidationError{
			Field:   "Account",
			Message: "Account is required",
		})
	}
	
	if o.Quantity <= 0 {
		errors = append(errors, ValidationError{
			Field:   "Quantity",
			Message: "Quantity must be greater than 0",
		})
	}
	
	// Validate price based on order type
	switch o.OrderType {
	case TypeMarket:
		// Market orders don't require price
	case TypeLimit:
		if o.Price <= 0 {
			errors = append(errors, ValidationError{
				Field:   "Price",
				Message: "Price is required for limit orders and must be greater than 0",
			})
		}
	case TypeStop:
		if o.StopPrice <= 0 {
			errors = append(errors, ValidationError{
				Field:   "StopPrice",
				Message: "StopPrice is required for stop orders and must be greater than 0",
			})
		}
	case TypeStopLimit:
		if o.Price <= 0 {
			errors = append(errors, ValidationError{
				Field:   "Price",
				Message: "Price is required for stop-limit orders and must be greater than 0",
			})
		}
		if o.StopPrice <= 0 {
			errors = append(errors, ValidationError{
				Field:   "StopPrice",
				Message: "StopPrice is required for stop-limit orders and must be greater than 0",
			})
		}
	default:
		errors = append(errors, ValidationError{
			Field:   "OrderType",
			Message: "Invalid order type",
		})
	}
	
	// Validate side
	if o.Side != SideBuy && o.Side != SideSell {
		errors = append(errors, ValidationError{
			Field:   "Side",
			Message: "Side must be either BUY or SELL",
		})
	}
	
	return errors
}

// ValidateExecution checks if an execution is valid for an order
func (o *Order) ValidateExecution(execQty float64) error {
	// Check order status
	if o.Status == StatusCanceled || o.Status == StatusRejected || o.Status == StatusExpired {
		return errors.New("cannot execute an order that is canceled, rejected, or expired")
	}
	
	// Check if execution quantity would exceed remaining quantity
	remainingQty := o.Quantity - o.FilledQty
	if execQty > remainingQty {
		return fmt.Errorf("execution quantity %f exceeds remaining order quantity %f", 
			execQty, remainingQty)
	}
	
	return nil
}
