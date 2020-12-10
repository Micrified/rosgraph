#if !defined(ROSLOG_H)
#define ROSLOG_H

void roslog_init ();

int roslog_chain_from_topic (std::string topic);

void roslog_log (int chain, int callback, bool start);

void roslog_close ();

#endif