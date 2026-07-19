package service

import (
	"context"

	"github.com/finverse/customer-service/internal/domain"
)

// In-memory fakes for the repository/storage interfaces, used by the unit
// tests in this package so business logic is verified without a real
// database — per docs/17-testing-strategy.md §2 ("never mock the thing
// you're testing"; these are the boundary, not the logic under test). Mirrors
// Auth Service's internal/service/fakes_test.go pattern.

type fakeCustomerRepo struct {
	byID     map[string]*domain.Customer
	byUserID map[string]*domain.Customer
}

func newFakeCustomerRepo() *fakeCustomerRepo {
	return &fakeCustomerRepo{byID: map[string]*domain.Customer{}, byUserID: map[string]*domain.Customer{}}
}

func (f *fakeCustomerRepo) Create(_ context.Context, c *domain.Customer) error {
	if _, exists := f.byUserID[c.UserID]; exists {
		return domain.ErrProfileAlreadyExists
	}
	cp := *c
	f.byID[c.ID] = &cp
	f.byUserID[c.UserID] = &cp
	return nil
}

func (f *fakeCustomerRepo) GetByID(_ context.Context, id string) (*domain.Customer, error) {
	if c, ok := f.byID[id]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, domain.ErrCustomerNotFound
}

func (f *fakeCustomerRepo) GetByUserID(_ context.Context, userID string) (*domain.Customer, error) {
	if c, ok := f.byUserID[userID]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, domain.ErrCustomerNotFound
}

func (f *fakeCustomerRepo) UpdateProfile(_ context.Context, c *domain.Customer) error {
	if _, ok := f.byID[c.ID]; !ok {
		return domain.ErrCustomerNotFound
	}
	cp := *c
	f.byID[c.ID] = &cp
	f.byUserID[c.UserID] = &cp
	return nil
}

func (f *fakeCustomerRepo) UpdateKYC(_ context.Context, customerID string, status domain.KYCStatus, tier domain.KYCTier) error {
	c, ok := f.byID[customerID]
	if !ok {
		return domain.ErrCustomerNotFound
	}
	c.KYCStatus = status
	c.KYCTier = tier
	f.byUserID[c.UserID] = c
	return nil
}

type fakeDocumentRepo struct {
	byCustomerID map[string][]*domain.KYCDocument
}

func newFakeDocumentRepo() *fakeDocumentRepo {
	return &fakeDocumentRepo{byCustomerID: map[string][]*domain.KYCDocument{}}
}

func (f *fakeDocumentRepo) Create(_ context.Context, d *domain.KYCDocument) error {
	cp := *d
	f.byCustomerID[d.CustomerID] = append(f.byCustomerID[d.CustomerID], &cp)
	return nil
}

func (f *fakeDocumentRepo) ListByCustomerID(_ context.Context, customerID string) ([]*domain.KYCDocument, error) {
	return f.byCustomerID[customerID], nil
}

type fakeBeneficiaryRepo struct {
	byID         map[string]*domain.Beneficiary
	byCustomerID map[string][]string
}

func newFakeBeneficiaryRepo() *fakeBeneficiaryRepo {
	return &fakeBeneficiaryRepo{byID: map[string]*domain.Beneficiary{}, byCustomerID: map[string][]string{}}
}

func (f *fakeBeneficiaryRepo) Create(_ context.Context, b *domain.Beneficiary) error {
	cp := *b
	f.byID[b.ID] = &cp
	f.byCustomerID[b.CustomerID] = append(f.byCustomerID[b.CustomerID], b.ID)
	return nil
}

func (f *fakeBeneficiaryRepo) GetByID(_ context.Context, id string) (*domain.Beneficiary, error) {
	if b, ok := f.byID[id]; ok {
		cp := *b
		return &cp, nil
	}
	return nil, domain.ErrBeneficiaryNotFound
}

func (f *fakeBeneficiaryRepo) ListByCustomerID(_ context.Context, customerID string) ([]*domain.Beneficiary, error) {
	var result []*domain.Beneficiary
	for _, id := range f.byCustomerID[customerID] {
		cp := *f.byID[id]
		result = append(result, &cp)
	}
	return result, nil
}

func (f *fakeBeneficiaryRepo) MarkVerified(_ context.Context, id string) error {
	b, ok := f.byID[id]
	if !ok {
		return domain.ErrBeneficiaryNotFound
	}
	b.Verified = true
	return nil
}

type fakeStorage struct {
	saved map[string][]byte
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{saved: map[string][]byte{}}
}

func (f *fakeStorage) Save(_ context.Context, customerID, filename string, data []byte) (string, error) {
	key := customerID + "/" + filename
	f.saved[key] = data
	return key, nil
}
