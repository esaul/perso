package main

import (
	"net/mail"
	"sort"
	"strings"
)

type cacheString map[string]mailFiles

type caches struct {
	data      map[string]cacheString
	indexer   *mailIndexer
	mailCh    chan cacheMail
	listCh    chan cacheListRequest
	requestCh chan cacheRequest
	addCh     chan cacheEntry
	removeCh  chan mailFiles
}

type cacheRequest struct {
	header string
	value  string
	index  int
	limit  int
	oldest bool
	match  keyType
	data   chan mailFiles
}

type cacheListRequest struct {
	header string
	data   chan []string
}

type cacheEntry struct {
	name  string
	key   string
	value mailFile
}

type cacheMail struct {
	id      mailFile
	headers mail.Header
}

func newCacheRequest() *cacheRequest {
	return &cacheRequest{
		data: make(chan mailFiles),
	}
}

func newCacheListRequest() *cacheListRequest {
	return &cacheListRequest{
		data: make(chan []string),
	}
}

func newCaches(indexer *mailIndexer, root string) *caches {
	c := &caches{
		indexer:   indexer,
		data:      make(map[string]cacheString),
		listCh:    make(chan cacheListRequest),
		requestCh: make(chan cacheRequest),
		addCh:     make(chan cacheEntry),
		removeCh:  make(chan mailFiles),
	}
	for i := range indexer.keys {
		c.initCachesString(i)
	}

	return c
}

func (c *caches) initCachesString(name string) {
	c.data[name] = make(map[string]mailFiles)
}

func (c *caches) add(entry cacheEntry) {
	name, key, value := entry.name, entry.key, entry.value

	if _, found := c.data[name][key]; !found {
		c.data[name][key] = newMailFiles()
	}

	c.data[name][key] = append(c.data[name][key], value)
}

func (c *caches) remove(files mailFiles) {
	for name := range c.data {
		for k := range c.data[name] {
			sort.Sort(c.data[name][k])
			c.data[name][k] = c.data[name][k].diff(files)
		}
	}
}

func (c *caches) match(header, value string, match keyType) mailFiles {
	cache, found := c.data[header]
	if !found {
		return nil
	}

	if match == keyTypeAddr {
		value = strings.ToLower(value)
	}

	// Full match from cache
	if match != keyTypePart {
		return cache[value]
	}

	// Handle partial matches
	results := newMailFiles()

	for val, files := range cache {
		if strings.Contains(val, value) {
			for f := range files {
				results = append(results, files[f])
			}
		}
	}

	return results
}

func (c *caches) request(r cacheRequest) {
	c.requestCh <- r
}

func (c *caches) respond(r cacheRequest) {
	defer close(r.data)

	files := c.match(r.header, r.value, r.match)
	lfiles := len(files)
	if lfiles == 0 {
		return
	}

	if r.limit == 0 {
		r.limit = 1
	}
	if r.limit > lfiles {
		r.limit = lfiles
	}

	if r.index >= lfiles {
		return
	}

	if !r.oldest {
		sort.Sort(sort.Reverse(files))
	} else {
		sort.Sort(files)
	}

	// Copy only elements between r.index and r.index + r.limit
	result := mailFiles(make([]mailFile, r.limit))
	for i, j := 0, r.index; i < r.limit; i, j = i+1, j+1 {
		result[i] = files[j]
	}

	r.data <- result
}

func (c *caches) list(r cacheListRequest) {
	defer close(r.data)

	values, found := c.data[r.header]
	if !found {
		return
	}

	keys := make([]string, len(values))
	i := 0
	for k := range values {
		keys[i] = k
		i = i + 1
	}

	r.data <- keys
}

func (c *caches) run() {
	for {
		select {
		case r := <-c.listCh:
			c.list(r)
		case r := <-c.requestCh:
			c.respond(r)
		case entry := <-c.addCh:
			c.add(entry)
		case files := <-c.removeCh:
			c.remove(files)
		}
	}
}
