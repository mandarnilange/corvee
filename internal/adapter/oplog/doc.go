// Package oplog is the operation journal (write-ahead log) for
// multi-file mutations such as move, rename, delete --cascade, and
// clone --with-children. An intent record is written before any step
// runs; recover() rolls forward incomplete operations after a crash.
package oplog
