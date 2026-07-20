package http

import (
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/wallet-service/internal/domain"
	"github.com/gin-gonic/gin"
)

func writeDomainError(c *gin.Context, err error) {
	switch err {
	case domain.ErrWalletNotFound:
		apiresponse.NotFound(c, "WALLET_NOT_FOUND", "Wallet not found.")
	case domain.ErrWalletAlreadyExists:
		apiresponse.Conflict(c, "WALLET_ALREADY_EXISTS", "You already have a wallet.")
	case domain.ErrWalletClosed:
		apiresponse.UnprocessableEntity(c, "WALLET_CLOSED", "This wallet is closed.")
	case domain.ErrInsufficientBalance:
		apiresponse.UnprocessableEntity(c, "INSUFFICIENT_BALANCE", "This transfer would take your wallet balance negative.")
	case domain.ErrSameWallet:
		apiresponse.BadRequest(c, "SAME_WALLET", "You can't transfer to your own wallet.")
	case domain.ErrInvalidAmount:
		apiresponse.BadRequest(c, "INVALID_AMOUNT", "Amount must be a positive number.")
	default:
		apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
	}
}
