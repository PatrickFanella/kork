# Kork Stripe catalog

Prepared October 8, 2026 in the owner-selected UNDERTOW account, `acct_1ULZ9hElO9mtgE8Q`. These identifiers are not credentials. Kork's production billing remains disabled until a restricted server key is installed and subscription, webhook and cancellation behavior is verified in test mode.

| Setting | Test mode | Live mode |
| --- | --- | --- |
| Product | `prod_kork_pro` | `prod_kork_pro` |
| Monthly price, USD 5 | `price_1UOPtUElO9mtgE8QUPoR6Bkx` | `price_1UOPvlElO9mtgE8QL0AtAMV2` |
| Annual price, USD 48 | `price_1UOPtUElO9mtgE8QAF3twoxt` | `price_1UOPvlElO9mtgE8QTO4OLiWC` |
| Customer portal configuration | `bpc_1UOPuaElO9mtgE8Q1Cz7d2lg` | `bpc_1UOPvmElO9mtgE8Qq9xzN8LP` |
| Webhook endpoint | `we_1UOPwWElO9mtgE8Q3BvmxgTr` (disabled) | Not created |

The portal permits invoice history, payment-method updates and cancellation at the end of the billing period. Plan switching is off. It uses Kork's terms, privacy and settings URLs. No existing portal configurations were present when these were created.

The test webhook targets `https://kork.subcult.tv/stripe/webhook` and uses API version `2026-09-30.endive`. Its signing secret is stored in the private qualification receipt directory, outside this repository. Keep the endpoint disabled until a test receiver is configured; the public service currently has no Stripe credentials.

Stripe accepted a test Checkout Session with the monthly price and Kork's metadata, and the hosted page displayed Kork Pro at $5/month in sandbox mode. The session was created through the connector, so this does not qualify the application's API authentication, webhook delivery or Pro entitlement. No live Checkout, subscription or charge was created.

UNDERTOW defaults to Managed Payments. Kork explicitly disables it per Checkout Session for direct Stripe Billing. The initial test request that inherited the default failed because Managed Payments required a product tax code; the explicit direct-billing request succeeded. Shared account defaults were not changed.

Complete the remaining steps in [Billing setup](billing.md). SUBCULT Backer eligibility still needs the Patreon client, campaign and tier identifiers, followed by an actual OAuth connection test.
