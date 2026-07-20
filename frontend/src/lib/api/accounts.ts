// Typed client for Account Service (docs/07-microservice-specifications.md §4).
import { ACCOUNT_SERVICE_URL } from '$lib/config';
import { apiRequest } from './http';

export interface Account {
	id: string;
	accountNumber: string;
	productType: 'SAVINGS' | 'CURRENT' | 'JOINT' | 'VIRTUAL';
	status: 'ACTIVE' | 'FROZEN' | 'CLOSED';
	overdraftLimitMinor: number;
}

export interface Balance {
	accountId: string;
	ledgerBalanceMinor: number;
	availableBalanceMinor: number;
}

export interface StatementEntry {
	transactionId: string;
	legId: string;
	amountMinor: number;
	direction: 'DIRECTION_DEBIT' | 'DIRECTION_CREDIT';
	currency: string;
}

export function listMyAccounts(): Promise<Account[]> {
	return apiRequest<Account[]>(ACCOUNT_SERVICE_URL, '/api/v1/accounts', {
		method: 'GET',
		auth: true
	});
}

export function openAccount(productType: Account['productType']): Promise<Account> {
	return apiRequest<Account>(ACCOUNT_SERVICE_URL, '/api/v1/accounts', {
		method: 'POST',
		auth: true,
		body: { productType }
	});
}

export function getBalance(accountId: string): Promise<Balance> {
	return apiRequest<Balance>(ACCOUNT_SERVICE_URL, `/api/v1/accounts/${accountId}/balance`, {
		method: 'GET',
		auth: true
	});
}

export function getStatement(accountId: string): Promise<StatementEntry[]> {
	return apiRequest<StatementEntry[]>(
		ACCOUNT_SERVICE_URL,
		`/api/v1/accounts/${accountId}/statement`,
		{
			method: 'GET',
			auth: true
		}
	);
}
