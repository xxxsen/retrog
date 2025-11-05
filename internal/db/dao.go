package db

import "github.com/xxxsen/common/database"

// DatabaseGetter returns a database handle. Used to defer retrieval until first use.
type DatabaseGetter func() database.IDatabase
