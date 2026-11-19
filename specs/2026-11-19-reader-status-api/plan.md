# Reader Status API Plan

## 1. Confirm the Agent contract and configuration

- Match the request and response fields in README section 11.
- Define the authentication header, missing-ID behavior, and request limits. Document the inbound key and listen address in the environment example. Verify interoperability with the Agent during deployment validation.

## 2. Add the PostgreSQL status lookup

- Add a read-only bulk lookup by file ID using parameterized SQL against file_management. Return only the columns required by the API.
- Test mixed reading and done rows, zero counts, missing IDs, and database errors.

## 3. Add the endpoint and wire startup

- Register POST /status on net/http.ServeMux. Authenticate before reading the body or querying PostgreSQL. Validate JSON and return structured request logs with secrets excluded.
- Start the HTTP server alongside Agent polling. Set request timeouts and shut the server down with the Reader. Document how the Agent reaches the endpoint in Docker Compose.

## 4. Verify the contract and deployment wiring

- Add handler tests using httptest and a PostgreSQL integration test built around the existing migrations.
- Run the checks in [validation.md](validation.md) and record the results before merge.
