# RSS data source framework

The retained connector is `connector/rss`, including its site adapters. It supports RSS, Atom and JSON Feed, optional encrypted authentication headers, full and incremental sync, and scheduled or manual jobs.

The scheduler enqueues an queue task. The service fetches source items, applies the existing diff and conflict rules, and sends new or updated entries through the entry parsing pipeline. `data_sources` stores configuration and cursors; `sync_logs` records each run. All data uses the fixed space ID `1`.

The connector registry, framework interfaces and scheduler remain in place. Extend site adapters inside the RSS package. See [RSS development guide](../../docs/datasource-development.md) for the API and configuration flow.
