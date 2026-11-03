"""Read actual executions for fixture cleanup/counts across CLI generations."""


def execution_records(cli, protocol):
    if protocol < 23:
        return cli("process", "list").get("items", [])
    arguments = ("process", "list", "--all-profiles", "--view", "history", "--limit", "200")
    page = cli(*arguments)
    rows = list(page["items"])
    while page.get("next_cursor"):
        page = cli(*arguments, "--cursor", page["next_cursor"])
        rows.extend(page["items"])
    return [row for row in rows if row.get("kind") == "execution"]
