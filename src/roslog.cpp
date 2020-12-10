#include <iostream>
#include <chrono>

extern "C" {
	#include <syslog.h>
	#include <errno.h>
	#include <string.h>
	#include <stdarg.h>
}

#define EXEC_NAME			"ROS"

void roslog_init ()
{
	openlog(EXEC_NAME, LOG_CONS, LOG_USER);
}

int roslog_chain_from_topic (std::string topic)
{
	std::string::size_type i;

	// Expect: All auto-generated topics have format: T_N<CALLBACK_ID>_N<CALBACK_ID>_C<CHAIN_ID>
	for (i = 0; i < topic.size(); ++i) {
		if (topic[i] == 'C') {
			break;
		}
	}

	// Case: Not found
	if (i >= (topic.size() - 1)) {
		return -1;
	}

	// Else, build the chain number
	int chain_id = topic[i+1] - '0';
	for (i = i + 2; i < topic.size(); ++i) {
		chain_id *= 10;
		chain_id = chain_id + (topic[i] - '0');
	}

	return chain_id;
}

void roslog_log (int chain, int callback, bool start)
{
	auto timestamp_ns = std::chrono::time_point_cast<std::chrono::nanoseconds>(
		std::chrono::steady_clock::now());
	auto value_ns = timestamp_ns.time_since_epoch();
	syslog(LOG_INFO, "{chain: %d, callback: %d, timestamp: %ld, start: %d}",
		chain, callback, value_ns.count(), start);
}

void roslog_close ()
{
	closelog();
}