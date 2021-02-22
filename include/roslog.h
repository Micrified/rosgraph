#if !defined(ROSLOG_H)
#define ROSLOG_H

void roslog_init ();

long roslog_get_timestamp ();

int roslog_chain_from_topic (std::string topic);

void roslog_log_chain (int executor, int chain, long start, long duration);

void roslog_log_callback (int executor, int chain, int callback, long start, long duration);

void roslog_log_value (long value);

void roslog_close ();

#endif