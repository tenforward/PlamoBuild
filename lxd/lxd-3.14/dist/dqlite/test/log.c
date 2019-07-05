#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "../include/dqlite.h"

#include "log.h"
#include "munit.h"

static void emit(void *ctx, int level, const char *format, va_list args) {
	char *  msg;
	int     err;

	(void)ctx;
	(void)level;

	err = vasprintf(&msg, format, args);
	va_end(args);

	if (err < 0) {
		return;
	}

	munit_log(MUNIT_LOG_INFO, msg);

	free(msg);
}

dqlite_logger *test_logger() {
	dqlite_logger *logger = munit_malloc(sizeof *logger);

	logger->data   = NULL;
	logger->emit = emit;

	return logger;
}

struct test_log {
	FILE * stream;
	char * buffer;
	size_t size;
};

test_log *test_log_open() {
	test_log *log = (test_log *)malloc(sizeof(test_log));

	if (!log) {
		fprintf(stderr,
		        "\nFailed allocate test log object: %s\n",
		        strerror(errno));
		exit(EXIT_FAILURE);
	}

	log->stream = open_memstream(&log->buffer, &log->size);

	if (!log->stream) {
		fprintf(stderr,
		        "\nFailed to open test log memory stream: %s\n",
		        strerror(errno));
		exit(EXIT_FAILURE);
	}

	return log;
}

FILE *test_log_stream(test_log *log) {
	return log->stream;
}

int test_log_is_empty(test_log *log) {
	return log->size == 0;
}

char *test_log_output(test_log *log) {
	return log->buffer;
}

void test_log_close(test_log *log) {
	int err;

	err = fclose(log->stream);

	if (err) {
		fprintf(stderr,
		        "\nFailed to close test log memory stream: %s\n",
		        strerror(err));
		exit(EXIT_FAILURE);
	}
}

void test_log_destroy(test_log *log) {
	free(log->buffer);
	free(log);
}
