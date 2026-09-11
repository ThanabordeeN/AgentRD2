#pragma once

#include <atomic>
#include <condition_variable>
#include <deque>
#include <functional>
#include <mutex>
#include <string>
#include <thread>

#include "bridge_protocol.hpp"
#include "mini_json.hpp"

namespace rdr2ai {

// Non-blocking, line-oriented TCP JSON client.
//
// The game thread only enqueues messages.  A worker thread performs
// connect/send/receive and invokes the callback, so the game stays responsive.
class IpcClient {
public:
    using MessageCallback = std::function<void(const std::string& line)>;

    IpcClient(std::string host, unsigned short port);
    ~IpcClient();

    bool start(MessageCallback on_message);
    void stop();

    // Thread-safe send methods.
    void send_json(const Json& payload);
    void send_action_result(const ActionResult& result);
    void send_ped_scan(const std::string& peds_json);
    void send_event(const Json& event_json);

private:
    void worker_loop();
    void enqueue(std::string line);
    void send_queued(class Socket& socket);

    std::string host_;
    unsigned short port_;
    std::atomic<bool> running_{false};
    std::thread worker_;
    MessageCallback on_message_;

    std::mutex queue_mutex_;
    std::condition_variable queue_cv_;
    std::deque<std::string> queue_;
};

}  // namespace rdr2ai
