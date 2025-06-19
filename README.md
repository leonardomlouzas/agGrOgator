# agGrOgator

RSS feed aggregator made in GO.

- You need GO and Postgres installed beforehand.

## Instructions

To install it, simply run:
`go install ...`

Then create a `.gatorconfig.json` file in your home directory:

```json
{
  "db_url": "postgres://username:@localhost:5432/database?sslmode=disable"
}
```

- Replace the credentials with your own.

## Usage

### Users

```
gator register <name>
```

```
gator login <name>
```

```
gator users
```

### Feeds:

```
gator addfeed <url>
```

```
gator feeds
```

```
gator follow <url>
```

```
gator unfollow <url>
```

```
gator browse <limit>
```

### Start the aggregator:

```
gator agg 60s
```
