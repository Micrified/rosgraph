#include <iostream>
#include <chrono>
#include "tacle_benchmarks.h"

extern "C" {
	#include <syslog.h>
	#include <errno.h>
	#include <string.h>
	#include <stdarg.h>
}

#define N_TESTS     9

long get_timestamp ()
{
	auto timestamp_us = std::chrono::time_point_cast<std::chrono::microseconds>(
		std::chrono::steady_clock::now());
	auto value_us = timestamp_us.time_since_epoch();
	return value_us.count();	
}

long trial_function (int runs, int (*f)(void))
{
	double avg_execution_time = 0.0;
	for (int i = 0; i < runs; i++) {
		long start = get_timestamp();
		f();
		avg_execution_time += double(get_timestamp() - start);
	}
	avg_execution_time /= double(runs);
	return long(avg_execution_time);
}

int main (int argc, char const *argv[]) {
	int n_runs = 0;

	char const *test_names[N_TESTS] = {
		"adpcm_dec",
		"adpcm_enc",
		"dijkstra",
		"epic",
		"g723_enc",
		"huff_enc",
		"mpeg2",
		"ndes",
		"statemate"
	};

	int (*test_functions[N_TESTS])(void) = {
		adpcm_dec,
		adpcm_enc,
		dijkstra,
		epic,
		g723_enc,
		huff_enc,
		mpeg2,
		ndes,
		statemate
	};

	if (argc != 2) {
		std::cout << std::string(argv[0]) << " <n-runs>" << std::endl;
		return EXIT_SUCCESS;
	} else {
		n_runs = std::atoi(argv[1]);
	}
	
	std::cout << "[" << std::endl;
	for (int i = 0; i < N_TESTS; i++) {
		long average_execution_time = trial_function(n_runs, test_functions[i]);
		std::cout << "{\"" << std::string(test_names[i]) << "\":" << 
			std::to_string(average_execution_time) << "}";
		if (i < (N_TESTS - 1)) {
			std::cout << ",";
		}
		std::cout << std::endl;
	}
	std::cout << "]" << std::endl;

	return EXIT_SUCCESS;
}