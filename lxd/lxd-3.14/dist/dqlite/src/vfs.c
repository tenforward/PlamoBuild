#include <errno.h>
#include <pthread.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/time.h>

#include <sqlite3.h>

#include "../include/dqlite.h"

#include "./lib/assert.h"

#include "format.h"
#include "log.h"

/* Maximum pathname length supported by this VFS. */
#define VFS__MAX_PATHNAME 512

/* Maximum number of files this VFS can create. */
#define VFS__MAX_FILES 64

/* Hold content for a single page or frame in a volatile file. */
struct page
{
	void *buf; /* Content of the page. */
	void *hdr; /* Page header (only for WAL pages). */
};

/* Create a new volatile page for a database or WAL file.
 *
 * If it's a page for a WAL file, the WAL header will also be allocated. */
static struct page *page_create(int size, int wal)
{
	struct page *p;

	assert(size > 0);
	assert(wal == 0 || wal == 1);

	p = sqlite3_malloc(sizeof *p);
	if (p == NULL) {
		goto oom;
	}

	p->buf = sqlite3_malloc(size);
	if (p->buf == NULL) {
		goto oom_after_page_alloc;
	}
	memset(p->buf, 0, size);

	if (wal) {
		p->hdr = sqlite3_malloc(FORMAT__WAL_FRAME_HDR_SIZE);
		if (p->hdr == NULL) {
			goto oom_after_buf_malloc;
		}
		memset(p->hdr, 0, FORMAT__WAL_FRAME_HDR_SIZE);
	} else {
		p->hdr = NULL;
	}

	return p;

oom_after_buf_malloc:
	sqlite3_free(p->buf);

oom_after_page_alloc:
	sqlite3_free(p);

oom:
	return NULL;
}

/* Destroy a volatile page */
static void page_destroy(struct page *p)
{
	assert(p != NULL);
	assert(p->buf != NULL);

	sqlite3_free(p->buf);

	if (p->hdr != NULL) {
		sqlite3_free(p->hdr);
	}

	sqlite3_free(p);
}

/* Hold content for a shared memory mapping. */
struct shm
{
	void **regions;  /* Pointers to shared memory regions. */
	int regions_len; /* Number of shared memory regions. */

	unsigned shared[SQLITE_SHM_NLOCK];    /* Count of shared locks */
	unsigned exclusive[SQLITE_SHM_NLOCK]; /* Count of exclusive locks */
};

/* Create a new shared memory mapping for a database file. */
static struct shm *shm_create()
{
	struct shm *s;
	int i;

	s = sqlite3_malloc(sizeof *s);
	if (s == NULL) {
		goto oom;
	}

	s->regions = NULL;
	s->regions_len = 0;

	for (i = 0; i < SQLITE_SHM_NLOCK; i++) {
		s->shared[i] = 0;
		s->exclusive[i] = 0;
	}

	return s;

oom:
	return NULL;
}

/* Destroy a shared memory mapping. */
static void shm_destroy(struct shm *s)
{
	void *region;
	int i;

	assert(s != NULL);

	/* Free all regions. */
	for (i = 0; i < s->regions_len; i++) {
		region = *(s->regions + i);
		assert(region != NULL);
		sqlite3_free(region);
	}

	/* Free the shared memory region array. */
	if (s->regions != NULL) {
		sqlite3_free(s->regions);
	}

	sqlite3_free(s);
}

/* Hold content for a single file in the volatile file system. */
struct content
{
	char *filename;		/* Name of the file. */
	void *hdr;		/* File header (for WAL files). */
	struct page **pages;    /* All pages in the file. */
	int pages_len;		/* Number of pages in the file. */
	unsigned int page_size; /* Page size of each page. */

	int refcount; /* Number of open FDs referencing this file. */
	int type;     /* Content type (either main db or WAL). */

	struct shm *shm;     /* Shared memory (for db files). */
	struct content *wal; /* WAL file content (for db files). */

	dqlite_logger *logger; /* For error messages. */
};

/* Create the content structure for a new volatile file. */
static struct content *content_create(const char *name,
				      int type,
				      dqlite_logger *logger)
{
	struct content *c;

	assert(name != NULL);
	assert(type == FORMAT__DB || type == FORMAT__WAL ||
	       type == FORMAT__OTHER);

	c = sqlite3_malloc(sizeof *c);
	if (c == NULL) {
		goto oom;
	}

	c->logger = logger;

	// Copy the name, since when called from Go, the pointer will be freed.
	c->filename = sqlite3_malloc(strlen(name) + 1);
	if (c->filename == NULL) {
		goto oom_after_content_malloc;
	}
	strcpy(c->filename, name);

	// For WAL files, also allocate the WAL file header.
	if (type == FORMAT__WAL) {
		c->hdr = sqlite3_malloc(FORMAT__WAL_HDR_SIZE);
		if (c->hdr == NULL) {
			goto oom_after_filename_malloc;
		}
		memset(c->hdr, 0, FORMAT__WAL_HDR_SIZE);
	} else {
		c->hdr = NULL;
	}

	c->pages = 0;
	c->pages_len = 0;
	c->page_size = 0;
	c->refcount = 0;
	c->type = type;
	c->shm = NULL;
	c->wal = NULL;

	return c;

oom_after_filename_malloc:
	sqlite3_free(c->filename);

oom_after_content_malloc:
	sqlite3_free(c);

oom:
	return NULL;
}

/* Destroy the content of a volatile file. */
static void content_destroy(struct content *c)
{
	int i;
	struct page *page;

	assert(c != NULL);
	assert(c->filename != NULL);

	/* Free the filename. */
	sqlite3_free(c->filename);

	/* Free the header if it's a WAL file. */
	if (c->type == FORMAT__WAL) {
		assert(c->hdr != NULL);
		sqlite3_free(c->hdr);
	} else {
		assert(c->hdr == NULL);
	}

	/* Free all pages. */
	for (i = 0; i < c->pages_len; i++) {
		page = *(c->pages + i);
		assert(page != NULL);
		page_destroy(page);
	}

	/* Free the page array. */
	if (c->pages != NULL) {
		sqlite3_free(c->pages);
	}

	/* Free the SHM mappping */
	if (c->shm != NULL) {
		assert(c->type == FORMAT__DB);
		shm_destroy(c->shm);
	}

	sqlite3_free(c);
}

/* Return 1 if this file has no content. */
static int content_is_empty(struct content *c)
{
	assert(c != NULL);

	if (c->pages_len == 0) {
		assert(c->pages == NULL);
		return 1;
	}

	// If it was written, a page list and a page size must have been set.
	assert(c->pages != NULL && c->pages_len > 0 && c->page_size > 0);

	return 0;
}

// Get a page from this file, possibly creating a new one.
static int content_page_get(struct content *c, int pgno, struct page **page)
{
	int rc;
	int is_wal;

	assert(c != NULL);
	assert(pgno > 0);

	is_wal = c->type == FORMAT__WAL;

	/* SQLite should access pages progressively, without jumping more than
	 * one page after the end. */
	if (pgno > (c->pages_len + 1)) {
		dqlite__errorf(c,
			       "can't write page %d of file %s "
			       "which has only %d pages",
			       pgno, c->filename, c->pages_len);
		rc = SQLITE_IOERR_WRITE;
		goto err;
	}

	if (pgno == (c->pages_len + 1)) {
		/* Create a new page, grow the page array, and append the
		 * new page to it. */
		struct page **pages; /* New page array. */

		/* We assume that the page size has been set, either by
		 * intercepting the first main database file write, or by
		 * handling a 'PRAGMA page_size=N' command in
		 * vfs__file_control(). This assumption is enforced in
		 * vfs__write(). */
		assert(c->page_size > 0);

		*page = page_create(c->page_size, is_wal);
		if (*page == NULL) {
			rc = SQLITE_NOMEM;
			goto err;
		}

		pages = sqlite3_realloc(c->pages, (sizeof *pages) * pgno);
		if (pages == NULL) {
			rc = SQLITE_NOMEM;
			goto err_after_page_create;
		}

		/* Append the new page to the new page array. */
		*(pages + pgno - 1) = *page;

		/* Update the page array. */
		c->pages = pages;
		c->pages_len = pgno;
	} else {
		/* Return the existing page. */
		assert(c->pages != NULL);
		*page = *(c->pages + pgno - 1);
	}

	return SQLITE_OK;

err_after_page_create:
	page_destroy(*page);

err:
	*page = NULL;

	return rc;
}

/* Lookup a page from this file, returning NULL if it doesn't exist. */
static struct page *content_page_lookup(struct content *c, int pgno)
{
	struct page *page;

	assert(c != NULL);
	assert(pgno > 0);

	if (pgno > c->pages_len) {
		/* This page hasn't been written yet. */
		return NULL;
	}

	page = *(c->pages + pgno - 1);

	assert(page != NULL);

	if (c->type == FORMAT__WAL) {
		assert(page->hdr != NULL);
	}

	return page;
}

/* Truncate the file to be exactly the given number of pages. */
static void content_truncate(struct content *content, int pages_len)
{
	struct page **cursor;
	int i;

	/* We expect callers to only invoke us if some actual content has been
	 * written already. */
	assert(content->pages_len > 0);

	/* Truncate should always shrink a file. */
	assert(pages_len <= content->pages_len);
	assert(content->pages != NULL);

	/* Destroy pages beyond pages_len. */
	cursor = content->pages + pages_len;
	for (i = 0; i < (content->pages_len - pages_len); i++) {
		page_destroy(*cursor);
		cursor++;
	}

	/* Reset the file header (for WAL files). */
	if (content->type == FORMAT__WAL) {
		/* We expect callers to always truncate the WAL to zero. */
		assert(pages_len == 0);
		assert(content->hdr != NULL);
		memset(content->hdr, 0, FORMAT__WAL_HDR_SIZE);
	} else {
		assert(content->hdr == NULL);
	}

	/* Shrink the page array, possibly to 0.
	 *
	 * TODO: in principle realloc could fail also when shrinking. */
	content->pages = sqlite3_realloc(
	    content->pages, (sizeof *(content->pages)) * pages_len);

	/* Update the page count. */
	content->pages_len = pages_len;
}

/* Implementation of the abstract sqlite3_file base class. */
struct vfs__file
{
	sqlite3_file base;       /* Base class. Must be first. */
	struct root *root;       /* Pointer to volatile VFS data. */
	struct content *content; /* Handle to the file content. */
	int flags;		 /* Flags passed to xOpen */
	sqlite3_file *temp;      /* For temp-files, actual VFS. */
};

/* Root of the volatile file system. Contains pointers to the content
 * of all files that were created. */
struct root
{
	dqlite_logger *logger;     /* Send log messages here. */
	struct content **contents; /* Files content */
	int contents_len;	  /* Number of files */
	pthread_mutex_t mutex;     /* Serialize to access */
	int error;		   /* Last error occurred. */
};

/* Create a new root object. */
static struct root *root_create(dqlite_logger *logger)
{
	struct root *r;
	int contents_size;
	int err;

	r = sqlite3_malloc(sizeof *r);
	if (r == NULL) {
		goto oom;
	}

	r->logger = logger;
	r->contents_len = VFS__MAX_FILES;

	contents_size = r->contents_len * sizeof *r->contents;

	r->contents = sqlite3_malloc(contents_size);
	if (r->contents == NULL) {
		goto oom_after_root_alloc;
	}

	memset(r->contents, 0, contents_size);

	err = pthread_mutex_init(&r->mutex, NULL);
	assert(err == 0); /* Docs say that pthread_mutex_init can't fail */

	return r;

oom_after_root_alloc:
	sqlite3_free(r);

oom:
	return NULL;
}

/* Release the memory used internally by root object.
 *
 * All file content will be de-allocated, so dangling open FDs against
 * those files will be broken.
 */
static void root_destroy(struct root *r)
{
	struct content **cursor; /* Iterator for r->contents */
	int i;

	assert(r != NULL);
	assert(r->contents != NULL);

	cursor = r->contents;

	/* The content array has been allocated and has at least one slot. */
	assert(cursor != NULL);
	assert(r->contents_len > 0);

	for (i = 0; i < r->contents_len; i++) {
		struct content *content = *cursor;
		if (content != NULL) {
			content_destroy(content);
		}
		cursor++;
	}

	sqlite3_free(r->contents);
}

/* Find a content object by name.
 *
 * Fill out and return its index if found, otherwise return the index
 * of a free slot (or -1, if there are no free slots).
 */
static int root_content_lookup(
    struct root *r,
    const char *filename,
    struct content **out  // OUT: content object or NULL
)
{
	struct content **cursor; /* Iterator for r->contents */
	int i;

	/* Index of the content or of a free slot in the contents array. */
	int free_slot = -1;

	assert(r != NULL);
	assert(filename != NULL);

	cursor = r->contents;

	// The content array has been allocated and has at least one slot.
	assert(cursor != NULL);
	assert(r->contents_len > 0);

	for (i = 0; i < r->contents_len; i++) {
		struct content *content = *cursor;
		if (content && strcmp(content->filename, filename) == 0) {
			// Found matching file.
			*out = content;
			return i;
		}
		if (content == NULL && free_slot == -1) {
			// Keep track of the index of this empty slot.
			free_slot = i;
		}
		cursor++;
	}

	// No matching content object.
	*out = 0;

	return free_slot;
}

/* Find the database content object associated with the given WAL file name. */
static int root_database_content_lookup(struct root *r,
					const char *wal_filename,
					struct content **out)
{
	struct content *content;
	int main_filename_len;
	char *main_filename;

	assert(r != NULL);
	assert(wal_filename != NULL);
	assert(out != NULL);

	*out = NULL; /* In case of errors */

	main_filename_len = strlen(wal_filename) - strlen("-wal") + 1;
	main_filename = sqlite3_malloc(main_filename_len);

	if (main_filename == NULL) {
		return SQLITE_NOMEM;
	}

	strncpy(main_filename, wal_filename, main_filename_len - 1);
	main_filename[main_filename_len - 1] = '\0';

	root_content_lookup(r, main_filename, &content);

	sqlite3_free(main_filename);

	if (content == NULL) {
		return SQLITE_CORRUPT;
	}

	*out = content;

	return SQLITE_OK;
}

/* Return the size of the database file whose WAL file has the given name.
 *
 * The size must have been previously set when this routine is called. */
static int root_database_page_size(struct root *r,
				   const char *wal_filename,
				   unsigned int *page_size)
{
	struct content *content;
	int err;

	assert(r != NULL);
	assert(wal_filename != NULL);
	assert(page_size != NULL);

	*page_size = 0; /* In case of errors. */

	err = root_database_content_lookup(r, wal_filename, &content);
	if (err != SQLITE_OK) {
		return err;
	}

	assert(content->page_size > 0);

	*page_size = content->page_size;

	return SQLITE_OK;
}

static int vfs__delete_content(struct root *root, const char *filename)
{
	struct content *content;
	int content_index;
	int rc;

	/* Check if the file exists. */
	content_index = root_content_lookup(root, filename, &content);
	if (content == NULL) {
		root->error = ENOENT;
		rc = SQLITE_IOERR_DELETE_NOENT;
		goto err;
	}

	/* Check that there are no consumers of this file. */
	if (content->refcount > 0) {
		root->error = EBUSY;
		pthread_mutex_unlock(&root->mutex);
		rc = SQLITE_IOERR_DELETE;
		goto err;
	}

	/* Free all memory allocated for this file. */
	content_destroy(content);

	/* Reset the file content slot. */
	*(root->contents + content_index) = NULL;

	pthread_mutex_unlock(&root->mutex);

	return SQLITE_OK;

err:
	assert(rc != SQLITE_OK);

	return rc;
}

static int vfs__close(sqlite3_file *file)
{
	struct vfs__file *f = (struct vfs__file *)file;
	struct root *root = (struct root *)(f->root);

	if (f->temp != NULL) {
		int rc;

		/* Close the actual temporary file. */
		rc = f->temp->pMethods->xClose(f->temp);
		sqlite3_free(f->temp);

		return rc;
	}

	pthread_mutex_lock(&root->mutex);

	assert(f->content->refcount);
	f->content->refcount--;

	/* If we got zero references, free the shared memory mapping, if
	 * present. */
	if (f->content->refcount == 0 && f->content->shm != NULL) {
		shm_destroy(f->content->shm);
		f->content->shm = NULL;
	}

	if (f->flags & SQLITE_OPEN_DELETEONCLOSE) {
		vfs__delete_content(root, f->content->filename);
	}

	pthread_mutex_unlock(&root->mutex);

	return SQLITE_OK;
}

static int vfs__read(sqlite3_file *file,
		     void *buf,
		     int amount,
		     sqlite_int64 offset)
{
	struct vfs__file *f = (struct vfs__file *)file;

	int pgno;
	struct page *page;

	assert(buf != NULL);
	assert(amount > 0);
	assert(f != NULL);

	if (f->temp != NULL) {
		/* Read from the actual temporary file. */
		return f->temp->pMethods->xRead(f->temp, buf, amount, offset);
	}

	assert(f->content != NULL);
	assert(f->content->filename != NULL);
	assert(f->content->refcount > 0);

	/* From SQLite docs:
	 *
	 *   If xRead() returns SQLITE_IOERR_SHORT_READ it must also fill
	 *   in the unread portions of the buffer with zeros.  A VFS that
	 *   fails to zero-fill short reads might seem to work.  However,
	 *   failure to zero-fill short reads will eventually lead to
	 *   database corruption.
	 */

	/* Check if the file is empty. */
	if (content_is_empty(f->content)) {
		memset(buf, 0, amount);
		return SQLITE_IOERR_SHORT_READ;
	}

	/* From this point on we can assume that the file was written at least
	 * once. */

	/* Since writes to all files other than the main database or the WAL are
	 * no-ops and the associated content object remains empty, we expect the
	 * content type to be either FORMAT__DB or
	 * FORMAT__WAL. */
	assert(f->content->type == FORMAT__DB ||
	       f->content->type == FORMAT__WAL);

	switch (f->content->type) {
		case FORMAT__DB:
			/* Main database */

			/* If the main database file is not empty, we expect the
			 * page size to have been set by an initial write. */
			assert(f->content->page_size > 0);

			if (offset < f->content->page_size) {
				/* Reading from page 1. We expect the read to be
				 * at most page_size bytes. */
				assert(amount <= (int)f->content->page_size);

				pgno = 1;
			} else {
				/* For pages greater than 1, we expect a full
				 * page read, with an offset that starts exectly
				 * at the page boundary. */
				assert(amount == (int)f->content->page_size);
				assert((offset % f->content->page_size) == 0);

				pgno = (offset / f->content->page_size) + 1;
			}

			assert(pgno > 0);

			page = content_page_lookup(f->content, pgno);

			if (pgno == 1) {
				/* Read the desired part of page 1. */
				memcpy(buf, page->buf + offset, amount);
			} else {
				/* Read the full page. */
				memcpy(buf, page->buf, amount);
			}
			return SQLITE_OK;

		case FORMAT__WAL:
			/* WAL file */

			if (f->content->page_size == 0) {
				/* If the page size hasn't been set yet, set it
				 * by copy the one from the associated main
				 * database file. */
				int err = root_database_page_size(
				    f->root, f->content->filename,
				    &f->content->page_size);
				if (err != 0) {
					return err;
				}
			}

			if (offset == 0) {
				/* Read the header. */
				assert(amount == FORMAT__WAL_HDR_SIZE);
				assert(f->content->hdr != NULL);
				memcpy(buf, f->content->hdr,
				       FORMAT__WAL_HDR_SIZE);
				return SQLITE_OK;
			}

			/* For any other frame, we expect either a header read,
			 * a checksum read, a page read or a full frame read. */
			if (amount == FORMAT__WAL_FRAME_HDR_SIZE) {
				assert(((offset - FORMAT__WAL_HDR_SIZE) %
					(f->content->page_size +
					 FORMAT__WAL_FRAME_HDR_SIZE)) == 0);
				pgno = format__wal_calc_pgno(
				    f->content->page_size, offset);
			} else if (amount == sizeof(uint32_t) * 2) {
				if (offset == FORMAT__WAL_FRAME_HDR_SIZE) {
					/* Read the checksum from the WAL
					 * header. */
					memcpy(buf, f->content->hdr + offset,
					       amount);
					return SQLITE_OK;
				}
				assert(((offset - 16 - FORMAT__WAL_HDR_SIZE) %
					(f->content->page_size +
					 FORMAT__WAL_FRAME_HDR_SIZE)) == 0);
				pgno = (offset - 16 - FORMAT__WAL_HDR_SIZE) /
					   (f->content->page_size +
					    FORMAT__WAL_FRAME_HDR_SIZE) +
				       1;
			} else if (amount == (int)f->content->page_size) {
				assert(((offset - FORMAT__WAL_HDR_SIZE -
					 FORMAT__WAL_FRAME_HDR_SIZE) %
					(f->content->page_size +
					 FORMAT__WAL_FRAME_HDR_SIZE)) == 0);
				pgno = format__wal_calc_pgno(
				    f->content->page_size, offset);
			} else {
				assert(amount == (FORMAT__WAL_FRAME_HDR_SIZE +
						  (int)f->content->page_size));
				pgno = format__wal_calc_pgno(
				    f->content->page_size, offset);
			}

			if (pgno == 0) {
				// This is an attempt to read a page that was
				// never written.
				memset(buf, 0, amount);
				return SQLITE_IOERR_SHORT_READ;
			}

			page = content_page_lookup(f->content, pgno);

			if (amount == FORMAT__WAL_FRAME_HDR_SIZE) {
				memcpy(buf, page->hdr, amount);
			} else if (amount == sizeof(uint32_t) * 2) {
				memcpy(buf, page->hdr + 16, amount);
			} else if (amount == (int)f->content->page_size) {
				memcpy(buf, page->buf, amount);
			} else {
				memcpy(buf, page->hdr,
				       FORMAT__WAL_FRAME_HDR_SIZE);
				memcpy(buf + FORMAT__WAL_FRAME_HDR_SIZE,
				       page->buf, f->content->page_size);
			}

			return SQLITE_OK;
	}

	return SQLITE_IOERR_READ;
}

static int vfs__write(sqlite3_file *file,
		      const void *buf,
		      int amount,
		      sqlite_int64 offset)
{
	struct vfs__file *f = (struct vfs__file *)file;

	unsigned pgno;
	struct page *page;
	int rc;

	assert(buf != NULL);
	assert(amount > 0);
	assert(f != NULL);

	if (f->temp != NULL) {
		/* Write to the actual temporary file. */
		return f->temp->pMethods->xWrite(f->temp, buf, amount, offset);
	}

	assert(f->content != NULL);
	assert(f->content->filename != NULL);
	assert(f->content->refcount > 0);

	switch (f->content->type) {
		case FORMAT__DB:
			/* Main database. */
			if (offset == 0) {
				unsigned int page_size;

				/* This is the first database page. We expect
				 * the data to contain at least the header. */
				assert(amount >= FORMAT__DB_HDR_SIZE);

				/* Extract the page size from the header. */
				rc = format__get_page_size(FORMAT__DB, buf,
							   &page_size);
				if (rc != SQLITE_OK) {
					return rc;
				}

				if (f->content->page_size > 0) {
					/* Check that the given page size
					 * actually matches what we have
					 * recorded. Since we make 'PRAGMA
					 * page_size=N' fail if the page is
					 * already set (see struct
					 * vfs__fileControl), there should be
					 * no way for the user to change it. */
					assert(page_size ==
					       f->content->page_size);
				} else {
					/* This must be the very first write to
					 * the
					 * database. Keep track of the page
					 * size. */
					f->content->page_size = page_size;
				}

				pgno = 1;
			} else {
				/* The header must have been written and the
				 * page size set. */
				if (f->content->page_size == 0) {
					dqlite__errorf(f->content,
						       "first page of file %s "
						       "was not yet written",
						       f->content->filename);
					return SQLITE_IOERR_WRITE;
				}

				/* For pages beyond the first we expect offset
				 * to be a multiple of the page size. */
				assert((offset % f->content->page_size) == 0);

				/* We expect that SQLite writes a page at time.
				 */
				assert(amount == (int)f->content->page_size);

				pgno = (offset / f->content->page_size) + 1;
			}

			rc = content_page_get(f->content, pgno, &page);
			if (rc != SQLITE_OK) {
				return rc;
			}

			assert(page->buf != NULL);

			memcpy(page->buf, buf, amount);

			return SQLITE_OK;

		case FORMAT__WAL:
			/* WAL file. */

			if (f->content->page_size == 0) {
				/* If the page size hasn't been set yet, set it
				 * by copy the one from the associated main
				 * database file. */
				int err = root_database_page_size(
				    f->root, f->content->filename,
				    &f->content->page_size);
				if (err != 0) {
					return err;
				}
			}

			if (offset == 0) {
				/* This is the WAL header. */
				unsigned int page_size;
				int rc;

				/* We expect the data to contain exactly 32
				 * bytes. */
				assert(amount == FORMAT__WAL_HDR_SIZE);

				/* The page size indicated in the header must be
				 * valid
				 * and match the one of the database file. */
				rc = format__get_page_size(FORMAT__WAL, buf,
							   &page_size);
				if (rc != SQLITE_OK) {
					return SQLITE_CORRUPT;
				}

				if (page_size != f->content->page_size) {
					return SQLITE_CORRUPT;
				}

				memcpy(f->content->hdr, buf, amount);
				return SQLITE_OK;
			}

			assert(f->content->page_size > 0);

			/* This is a WAL frame write. We expect either a frame
			 * header or page write. */
			if (amount == FORMAT__WAL_FRAME_HDR_SIZE) {
				/* Frame header write. */
				assert(((offset - FORMAT__WAL_HDR_SIZE) %
					(f->content->page_size +
					 FORMAT__WAL_FRAME_HDR_SIZE)) == 0);

				pgno = format__wal_calc_pgno(
				    f->content->page_size, offset);

				content_page_get(f->content, pgno, &page);
				if (page == NULL) {
					return SQLITE_NOMEM;
				}
				memcpy(page->hdr, buf, amount);
			} else {
				/* Frame page write. */
				assert(amount == (int)f->content->page_size);
				assert(((offset - FORMAT__WAL_HDR_SIZE -
					 FORMAT__WAL_FRAME_HDR_SIZE) %
					(f->content->page_size +
					 FORMAT__WAL_FRAME_HDR_SIZE)) == 0);

				pgno = format__wal_calc_pgno(
				    f->content->page_size, offset);

				// The header for the this frame must already
				// have been written, so the page is there.
				page = content_page_lookup(f->content, pgno);

				assert(page != NULL);

				memcpy(page->buf, buf, amount);
			}

			return SQLITE_OK;

		case FORMAT__OTHER:
			// Silently swallow writes to any other file.
			return SQLITE_OK;
	}

	return SQLITE_IOERR_WRITE;
}

static int vfs__truncate(sqlite3_file *file, sqlite_int64 size)
{
	struct vfs__file *f = (struct vfs__file *)file;
	int pgno;

	assert(f != NULL);
	assert(f->content != NULL);

	/* We expect calls to xTruncate only for database and WAL files. */
	if (f->content->type != FORMAT__DB && f->content->type != FORMAT__WAL) {
		dqlite__errorf(f->content,
			       "truncate called on unexpected file %s",
			       f->content->filename);
		return SQLITE_IOERR_TRUNCATE;
	}

	/* Check if this file empty.*/
	if (content_is_empty(f->content)) {
		if (size > 0) {
			dqlite__errorf(f->content,
				       "truncate called to grow empty file %s",
				       f->content->filename);
			return SQLITE_IOERR_TRUNCATE;
		}

		/* Nothing to do. */
		return SQLITE_OK;
	}

	switch (f->content->type) {
		case FORMAT__DB:
			/* Main database. */

			/* Since the file size is not zero, some content must
			 * have been written and the page size must be known. */
			assert(f->content->page_size > 0);

			if ((size % f->content->page_size) != 0) {
				dqlite__errorf(f->content,
					       "truncate database file %s to "
					       "misaligned size %d",
					       f->content->filename, size);
				return SQLITE_IOERR_TRUNCATE;
			}

			pgno = size / f->content->page_size;
			break;

		case FORMAT__WAL:
			/* WAL file. */

			/* We expect SQLite to only truncate to zero, after a
			 * full checkpoint.
			 *
			 * TODO: figure out other case where SQLite might
			 * truncate to a different size.
			 */
			if (size != 0) {
				return SQLITE_PROTOCOL;
			}
			pgno = 0;
			break;
	}

	content_truncate(f->content, pgno);

	return SQLITE_OK;
}

static int vfs__sync(sqlite3_file *file, int flags)
{
	(void)file;
	(void)flags;

	return SQLITE_IOERR_FSYNC;
}

static int vfs__file_size(sqlite3_file *file, sqlite_int64 *size)
{
	struct vfs__file *f = (struct vfs__file *)file;

	/* Check if this file empty. */
	if (content_is_empty(f->content)) {
		*size = 0;
		return SQLITE_OK;
	}

	/* Since we don't allow writing any other file, this must be
	 * either a database file or WAL file. */
	assert(f->content->type == FORMAT__DB ||
	       f->content->type == FORMAT__WAL);

	/* Since this file is not empty, the page size must have been set. */
	assert(f->content->page_size > 0);

	switch (f->content->type) {
		case FORMAT__DB:
			*size = f->content->pages_len * f->content->page_size;
			break;

		case FORMAT__WAL:
			/* TODO? here we assume that FileSize() is never invoked
			 * between a header write and a page write. */
			*size = FORMAT__WAL_HDR_SIZE +
				(f->content->pages_len *
				 (FORMAT__WAL_FRAME_HDR_SIZE +
				  f->content->page_size));
			break;
	}

	return SQLITE_OK;
}

/* Locking a file is a no-op, since no other process has visibility on it. */
static int vfs__lock(sqlite3_file *file, int lock)
{
	(void)file;
	(void)lock;

	return SQLITE_OK;
}

/* Unlocking a file is a no-op, since no other process has visibility on it. */
static int vfs__unlock(sqlite3_file *file, int lock)
{
	(void)file;
	(void)lock;

	return SQLITE_OK;
}

/* We always report that a lock is held. This routine should be used only in
 * journal mode, so it doesn't matter. */
static int vfs__check_reserved_lock(sqlite3_file *file, int *result)
{
	(void)file;

	*result = 1;
	return SQLITE_OK;
}

/* Handle pragma a pragma file control. See the xFileControl
 * docstring in sqlite.h.in for more details. */
static int vfs__file_control_pragma(struct vfs__file *f, char **fnctl)
{
	const char *left;
	const char *right;

	assert(f != NULL);
	assert(fnctl != NULL);

	left = fnctl[1];
	right = fnctl[2];

	assert(left != NULL);

	if (strcmp(left, "page_size") == 0 && right) {
		/* When the user executes 'PRAGMA page_size=N' we save the
		 * size internally.
		 *
		 * The page size must be between 512 and 65536, and be a
		 * power of two. The check below was copied from
		 * sqlite3BtreeSetPageSize in btree.c.
		 *
		 * Invalid sizes are simply ignored, SQLite will do the same.
		 *
		 * It's not possible to change the size after it's set.
		 */
		int page_size = atoi(right);

		if (page_size >= FORMAT__PAGE_SIZE_MIN &&
		    page_size <= FORMAT__PAGE_SIZE_MAX &&
		    ((page_size - 1) & page_size) == 0) {
			if (f->content->page_size &&
			    page_size != (int)f->content->page_size) {
				fnctl[0] =
				    "changing page size is not supported";
				return SQLITE_IOERR;
			}
			f->content->page_size = page_size;
		}
	} else if (strcmp(left, "journal_mode") == 0 && right) {
		/* When the user executes 'PRAGMA journal_mode=x' we ensure
		 * that the desired mode is 'wal'. */
		if (strcasecmp(right, "wal") != 0) {
			fnctl[0] = "only WAL mode is supported";
			return SQLITE_IOERR;
		}
	}

	/* We're returning NOTFOUND here to tell SQLite that we wish it to go on
	 * with its own handling as well. If we returned SQLITE_OK the page size
	 * of the journal mode wouldn't be effectively set, as the processing of
	 * the PRAGMA would stop here. */
	return SQLITE_NOTFOUND;
}

static int vfs__file_control(sqlite3_file *file, int op, void *arg)
{
	struct vfs__file *f = (struct vfs__file *)file;

	switch (op) {
		case SQLITE_FCNTL_PRAGMA:
			return vfs__file_control_pragma(f, arg);
	}

	return SQLITE_OK;
}

static int vfs__sector_size(sqlite3_file *file)
{
	(void)file;

	return 0;
}

static int vfs__device_characteristics(sqlite3_file *file)
{
	(void)file;

	return 0;
}

/* Simulate shared memory by allocating on the C heap. */
static int shm_map(sqlite3_file *file, /* Handle open on database file */
		   int region_index,   /* Region to retrieve */
		   int region_size,    /* Size of regions */
		   int extend,	 /* True to extend file if necessary */
		   void volatile **out /* OUT: Mapped memory */
)
{
	struct vfs__file *f = (struct vfs__file *)file;
	void *region;
	int rc;

	if (f->content->shm == NULL) {
		f->content->shm = shm_create();
		if (f->content->shm == NULL) {
			rc = SQLITE_NOMEM;
			goto err;
		}
	}

	if (f->content->shm->regions != NULL &&
	    region_index < f->content->shm->regions_len) {
		/* The region was already allocated. */
		region = *(f->content->shm->regions + region_index);
		assert(region != NULL);
	} else {
		if (extend) {
			/* We should grow the map one region at a time. */
			assert(region_index == f->content->shm->regions_len);
			region = sqlite3_malloc(region_size);
			if (region == NULL) {
				rc = SQLITE_NOMEM;
				goto err;
			}

			memset(region, 0, region_size);

			f->content->shm->regions = sqlite3_realloc(
			    f->content->shm->regions,
			    sizeof(void *) * (region_index + 1));

			if (f->content->shm->regions == NULL) {
				rc = SQLITE_NOMEM;
				goto err_after_region_malloc;
			}

			*(f->content->shm->regions + region_index) = region;
			f->content->shm->regions_len++;

		} else {
			/* The region was not allocated and we don't have to
			 * extend the map. */
			region = NULL;
		}
	}

	*out = region;

	return SQLITE_OK;

err_after_region_malloc:
	sqlite3_free(region);

err:
	assert(rc != SQLITE_OK);

	*out = NULL;

	return rc;
}

static int shm_lock(sqlite3_file *file, int ofst, int n, int flags)
{
	struct vfs__file *f;
	int i;

	assert(file != NULL);

	/* Legal values for the offset and the range */
	assert(ofst >= 0 && ofst + n <= SQLITE_SHM_NLOCK);
	assert(n >= 1);
	assert(n == 1 || (flags & SQLITE_SHM_EXCLUSIVE) != 0);

	/* Legal values for the flags.
	 *
	 * See https://sqlite.org/c3ref/c_shm_exclusive.html. */
	assert(flags == (SQLITE_SHM_LOCK | SQLITE_SHM_SHARED) ||
	       flags == (SQLITE_SHM_LOCK | SQLITE_SHM_EXCLUSIVE) ||
	       flags == (SQLITE_SHM_UNLOCK | SQLITE_SHM_SHARED) ||
	       flags == (SQLITE_SHM_UNLOCK | SQLITE_SHM_EXCLUSIVE));

	/* This is a no-op since shared-memory locking is relevant only for
	 * inter-process concurrency. See also the unix-excl branch from
	 * upstream (git commit cda6b3249167a54a0cf892f949d52760ee557129). */

	f = (struct vfs__file *)file;

	assert(f->content != NULL);
	assert(f->content->shm != NULL);

	if (flags & SQLITE_SHM_UNLOCK) {
		unsigned *these_locks;
		unsigned *other_locks;

		if (flags & SQLITE_SHM_SHARED) {
			these_locks = f->content->shm->shared;
			other_locks = f->content->shm->exclusive;
		} else {
			these_locks = f->content->shm->exclusive;
			other_locks = f->content->shm->shared;
		}

		for (i = ofst; i < ofst + n; i++) {
			/* Sanity check that no lock of the other type is held
			 * in this region. */
			assert(other_locks[i] == 0);

			/* Only decrease the lock count if it's positive. In
			 * other words releasing a never acquired lock is legal
			 * and idemponent. */
			if (these_locks[i] > 0) {
				these_locks[i]--;
			}
		}
	} else {
		if (flags & SQLITE_SHM_EXCLUSIVE) {
			/* No shared or exclusive lock must be held in the
			 * region. */
			for (i = ofst; i < ofst + n; i++) {
				if (f->content->shm->shared[i] > 0 ||
				    f->content->shm->exclusive[i] > 0) {
					return SQLITE_BUSY;
				}
			}

			for (i = ofst; i < ofst + n; i++) {
				assert(f->content->shm->exclusive[i] == 0);
				f->content->shm->exclusive[i] = 1;
			}
		} else {
			/* No exclusive lock must be held in the region. */
			for (i = ofst; i < ofst + n; i++) {
				if (f->content->shm->exclusive[i] > 0) {
					return SQLITE_BUSY;
				}
			}

			for (i = ofst; i < ofst + n; i++) {
				f->content->shm->shared[i]++;
			}
		}
	}

	return SQLITE_OK;
}

static void shm_barrier(sqlite3_file *file)
{
	(void)file;
	/* This is a no-op since we expect SQLite to be compiled with mutex
	 * support (i.e. SQLITE_MUTEX_OMIT or SQLITE_MUTEX_NOOP are *not*
	 * defined, see sqliteInt.h). */
}

static int shm_unmap(sqlite3_file *file, int delete_flag)
{
	struct vfs__file *f;

	assert(file != NULL);

	(void)delete_flag;

	f = (struct vfs__file *)file;

	// If the shm pointer is NULL no shared memory mapping is set.
	if (f->content->shm == NULL) {
		return SQLITE_OK;
	}

	return SQLITE_OK;
}

static const sqlite3_io_methods io_methods = {
    2,				  // iVersion
    vfs__close,			  // xClose
    vfs__read,			  // xRead
    vfs__write,			  // xWrite
    vfs__truncate,		  // xTruncate
    vfs__sync,			  // xSync
    vfs__file_size,		  // xFileSize
    vfs__lock,			  // xLock
    vfs__unlock,		  // xUnlock
    vfs__check_reserved_lock,     // xCheckReservedLock
    vfs__file_control,		  // xFileControl
    vfs__sector_size,		  // xSectorSize
    vfs__device_characteristics,  // xDeviceCharacteristics
    shm_map,			  // xShmMap
    shm_lock,			  // xShmLock
    shm_barrier,		  // xShmBarrier
    shm_unmap,			  // xShmUnmap
    0,
    0,
};

static int vfs__open(sqlite3_vfs *vfs,
		     const char *filename,
		     sqlite3_file *file,
		     int flags,
		     int *out_flags)
{
	struct root *root;
	struct vfs__file *f;
	struct content *content;

	int free_slot = -1; /* Index of a free slot in root->acontent. */
	int exists = 0;     /* Whether the file exists already. */

	int type; /* File content type (e.g. database or WAL). */
	int rc;   /* Return code. */

	/* Flags */
	int exclusive = flags & SQLITE_OPEN_EXCLUSIVE;
	int create = flags & SQLITE_OPEN_CREATE;

	assert(vfs != NULL);
	assert(vfs->pAppData != NULL);
	assert(file != NULL);

	(void)out_flags;

	root = (struct root *)(vfs->pAppData);
	f = (struct vfs__file *)file;

	/* This signals SQLite to not call Close() in case we return an error.
	 */
	f->base.pMethods = 0;
	f->temp = NULL;

	/* Save the flags */
	f->flags = flags;

	/* From SQLite documentation:
	 *
	 * If the zFilename parameter to xOpen is a NULL pointer then xOpen
	 * must invent its own temporary name for the file. Whenever the
	 * xFilename parameter is NULL it will also be the case that the
	 * flags parameter will include SQLITE_OPEN_DELETEONCLOSE.
	 */
	if (filename == NULL) {
		assert(flags & SQLITE_OPEN_DELETEONCLOSE);

		/* Open an actual temporary file. */
		vfs = sqlite3_vfs_find("unix");
		assert(vfs != NULL);

		f->temp = sqlite3_malloc(vfs->szOsFile);
		if (f->temp == NULL) {
			root->error = ENOENT;
			return SQLITE_CANTOPEN;
		}
		rc = vfs->xOpen(vfs, NULL, f->temp, flags, out_flags);
		if (rc != SQLITE_OK) {
			sqlite3_free(f->temp);
			return rc;
		}

		f->base.pMethods = &io_methods;
		f->root = NULL;
		f->content = NULL;

		return SQLITE_OK;
	}

	pthread_mutex_lock(&root->mutex);

	/* Search if the file exists already, and (if it doesn't) if there are
	 * free slots. */
	free_slot = root_content_lookup(root, filename, &content);
	exists = content != NULL;

	/* If file exists, and the exclusive flag is on, then return an error.
	 *
	 * From sqlite3.h.in:
	 *
	 *   The SQLITE_OPEN_EXCLUSIVE flag is always used in conjunction with
	 *   the SQLITE_OPEN_CREATE flag, which are both directly analogous to
	 *   the O_EXCL and O_CREAT flags of the POSIX open() API.  The
	 *   SQLITE_OPEN_EXCLUSIVE flag, when paired with the
	 *   SQLITE_OPEN_CREATE, is used to indicate that file should always be
	 *   created, and that it is an error if it already exists.  It is not
	 *   used to indicate the file should be opened for exclusive access.
	 */
	assert(!SQLITE_OPEN_EXCLUSIVE || SQLITE_OPEN_CREATE);
	if (exists && exclusive && create) {
		root->error = EEXIST;
		rc = SQLITE_CANTOPEN;
		goto err;
	}

	if (!exists) {
		/* Check the create flag. */
		if (!create) {
			root->error = ENOENT;
			rc = SQLITE_CANTOPEN;
			goto err;
		}

		/* This is a new file, so try to create a new entry. */
		if (free_slot == -1) {
			/* No more file content slots available. */
			root->error = ENFILE;
			rc = SQLITE_CANTOPEN;
			goto err;
		}

		if (flags & SQLITE_OPEN_MAIN_DB) {
			type = FORMAT__DB;
		} else if (flags & SQLITE_OPEN_WAL) {
			type = FORMAT__WAL;
		} else {
			type = FORMAT__OTHER;
		}

		content = content_create(filename, type, root->logger);
		if (content == NULL) {
			root->error = ENOMEM;
			rc = SQLITE_NOMEM;
			goto err;
		}

		if (type == FORMAT__WAL) {
			/* An associated database file must have been opened. */
			struct content *database;
			rc = root_database_content_lookup(root, filename,
							  &database);
			if (rc != SQLITE_OK) {
				root->error = ENOMEM;
				goto err_after_content_create;
			}
			database->wal = content;
		}

		/* Save the new file content in a free entry of the root file
		 * content array. */
		*(root->contents + free_slot) = content;
	}

	// Populate the new file handle.
	f->base.pMethods = &io_methods;
	f->root = root;
	f->content = content;

	content->refcount++;

	pthread_mutex_unlock(&root->mutex);

	return SQLITE_OK;

err_after_content_create:
	content_destroy(content);

err:
	assert(rc != SQLITE_OK);

	pthread_mutex_unlock(&root->mutex);

	return rc;
}

static int vfs__delete(sqlite3_vfs *vfs, const char *filename, int dir_sync)
{
	struct root *root;
	int rc;

	assert(vfs != NULL);
	assert(vfs->pAppData != NULL);

	root = (struct root *)(vfs->pAppData);

	(void)dir_sync;

	pthread_mutex_lock(&root->mutex);

	rc = vfs__delete_content(root, filename);

	pthread_mutex_unlock(&root->mutex);

	return rc;
}

static int vfs__access(sqlite3_vfs *vfs,
		       const char *filename,
		       int flags,
		       int *result)
{
	struct root *root;
	struct content *content;

	assert(vfs != NULL);
	assert(vfs->pAppData != NULL);

	(void)flags;

	root = (struct root *)(vfs->pAppData);

	pthread_mutex_lock(&root->mutex);

	/* If the file exists, access is always granted. */
	root_content_lookup(root, filename, &content);
	if (content == NULL) {
		root->error = ENOENT;
		*result = 0;
	} else {
		*result = 1;
	}

	pthread_mutex_unlock(&root->mutex);

	return SQLITE_OK;
}

static int vfs__full_pathname(sqlite3_vfs *vfs,
			      const char *filename,
			      int pathname_len,
			      char *pathname)
{
	(void)vfs;

	// Just return the path unchanged.
	sqlite3_snprintf(pathname_len, pathname, "%s", filename);
	return SQLITE_OK;
}

static void *vfs__dl_open(sqlite3_vfs *vfs, const char *filename)
{
	(void)vfs;
	(void)filename;

	return 0;
}

static void vfs__dl_error(sqlite3_vfs *vfs, int nByte, char *zErrMsg)
{
	(void)vfs;

	sqlite3_snprintf(nByte, zErrMsg,
			 "Loadable extensions are not supported");
	zErrMsg[nByte - 1] = '\0';
}

static void (*vfs__dl_sym(sqlite3_vfs *vfs, void *pH, const char *z))(void)
{
	(void)vfs;
	(void)pH;
	(void)z;

	return 0;
}

static void vfs__dl_close(sqlite3_vfs *vfs, void *pHandle)
{
	(void)vfs;
	(void)pHandle;

	return;
}

static int vfs__randomness(sqlite3_vfs *vfs, int nByte, char *zByte)
{
	(void)vfs;
	(void)nByte;
	(void)zByte;

	/* TODO (is this needed?) */
	return SQLITE_OK;
}

static int vfs__sleep(sqlite3_vfs *vfs, int microseconds)
{
	(void)vfs;

	/* TODO (is this needed?) */
	return microseconds;
}

static int vfs__current_time_int64(sqlite3_vfs *vfs, sqlite3_int64 *piNow)
{
	static const sqlite3_int64 unixEpoch =
	    24405875 * (sqlite3_int64)8640000;
	struct timeval now;

	(void)vfs;

	gettimeofday(&now, 0);
	*piNow =
	    unixEpoch + 1000 * (sqlite3_int64)now.tv_sec + now.tv_usec / 1000;
	return SQLITE_OK;
}

static int vfs__current_time(sqlite3_vfs *vfs, double *piNow)
{
	// TODO: check if it's always safe to cast a double* to a
	// sqlite3_int64*.
	return vfs__current_time_int64(vfs, (sqlite3_int64 *)piNow);
}

static int vfs__get_last_error(sqlite3_vfs *vfs, int x, char *y)
{
	struct root *root = (struct root *)(vfs->pAppData);
	int rc;

	(void)vfs;
	(void)x;
	(void)y;

	pthread_mutex_lock(&root->mutex);
	rc = root->error;
	pthread_mutex_unlock(&root->mutex);

	return rc;
}

sqlite3_vfs *dqlite_vfs_create(const char *name, dqlite_logger *logger)
{
	sqlite3_vfs *vfs;

	assert(name != NULL);

	vfs = sqlite3_malloc(sizeof *vfs);
	if (vfs == NULL) {
		goto err;
	}

	vfs->iVersion = 2;
	vfs->szOsFile = sizeof(struct vfs__file);
	vfs->mxPathname = VFS__MAX_PATHNAME;
	vfs->pNext = NULL;

	/* Make a copy of the provided name, so clients can free the string if
	 * they need. */
	vfs->zName = sqlite3_malloc(strlen(name) + 1);
	if (vfs->zName == NULL) {
		goto err_after_vfs_malloc;
	}
	strcpy((char *)vfs->zName, name);

	vfs->pAppData = root_create(logger);
	if (vfs->pAppData == NULL) {
		goto err_after_name_copy;
	}

	vfs->xOpen = vfs__open;
	vfs->xDelete = vfs__delete;
	vfs->xAccess = vfs__access;
	vfs->xFullPathname = vfs__full_pathname;
	vfs->xDlOpen = vfs__dl_open;
	vfs->xDlError = vfs__dl_error;
	vfs->xDlSym = vfs__dl_sym;
	vfs->xDlClose = vfs__dl_close;
	vfs->xRandomness = vfs__randomness;
	vfs->xSleep = vfs__sleep;
	vfs->xCurrentTime = vfs__current_time;
	vfs->xGetLastError = vfs__get_last_error;
	vfs->xCurrentTimeInt64 = vfs__current_time_int64;

	return vfs;

err_after_name_copy:
	sqlite3_free((char *)vfs->zName);

err_after_vfs_malloc:
	sqlite3_free(vfs);

err:
	return NULL;
}

void dqlite_vfs_destroy(sqlite3_vfs *vfs)
{
	struct root *root;

	assert(vfs != NULL);

	root = (struct root *)(vfs->pAppData);
	root_destroy(root);

	sqlite3_free(root);
	sqlite3_free((char *)vfs->zName);
	sqlite3_free(vfs);
}
