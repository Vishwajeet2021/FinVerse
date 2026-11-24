-- Each service owns its own logical database, per docs/08-database-design.md §1
-- ("no service ever connects to another service's database") and
-- docs/14-aws-infrastructure.md §5 (one RDS instance hosting a separate
-- logical database per service in early phases).
CREATE DATABASE auth_db;
CREATE DATABASE customer_db;
CREATE DATABASE transaction_db;
CREATE DATABASE account_db;
CREATE DATABASE wallet_db;
CREATE DATABASE payment_db;
CREATE DATABASE notification_db;
CREATE DATABASE audit_db;
CREATE DATABASE fraud_db;
CREATE DATABASE admin_db;
CREATE DATABASE reporting_db;
