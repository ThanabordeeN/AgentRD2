#include "ipc_client.hpp"

#include <chrono>
#include <cstring>
#include <sstream>
#include <stdexcept>

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
using socket_handle = SOCKET;
static const socket_handle kInvalidSocket = INVALID_SOCKET;
#else
#include <arpa/inet.h>
#include <errno.h>
#include <netdb.h>
#include <netinet/in.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <unistd.h>
using socket_handle = int;
static const socket_handle kInvalidSocket = -1;
#endif

namespace rdr2ai {

class Socket {
public:
    Socket() = default;
    ~Socket() { close(); }

    bool connect_to(const std::string& host, unsigned short port) {
        close();
#ifdef _WIN32
        WSADATA wsa;
        if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) return false;
#endif
        addrinfo hints{};
        hints.ai_family = AF_UNSPEC;
        hints.ai_socktype = SOCK_STREAM;
        addrinfo* result = nullptr;
        std::string port_str = std::to_string(port);
        if (getaddrinfo(host.c_str(), port_str.c_str(), &hints, &result) != 0) return false;
        for (addrinfo* it = result; it != nullptr; it = it->ai_next) {
            handle_ = ::socket(it->ai_family, it->ai_socktype, it->ai_protocol);
            if (handle_ == kInvalidSocket) continue;
            if (::connect(handle_, it->ai_addr, static_cast<int>(it->ai_addrlen)) == 0) {
                freeaddrinfo(result);
                return true;
            }
            close();
        }
        freeaddrinfo(result);
        return false;
    }

    void close() {
        if (handle_ != kInvalidSocket) {
#ifdef _WIN32
            closesocket(handle_);
            WSACleanup();
#else
            ::close(handle_);
#endif
            handle_ = kInvalidSocket;
        }
    }

    bool valid() const { return handle_ != kInvalidSocket; }

    int wait_readable(int timeout_ms) {
        if (!valid()) return -1;
        fd_set read_set;
        FD_ZERO(&read_set);
        FD_SET(handle_, &read_set);
        timeval timeout{};
        timeout.tv_sec = timeout_ms / 1000;
        timeout.tv_usec = (timeout_ms % 1000) * 1000;
#ifdef _WIN32
        int result = select(0, &read_set, nullptr, nullptr, &timeout);
#else
        int result = select(handle_ + 1, &read_set, nullptr, nullptr, &timeout);
#endif
        return result;
    }

    int send_bytes(const char* data, int size) {
#ifdef _WIN32
        return ::send(handle_, data, size, 0);
#else
        return static_cast<int>(::send(handle_, data, static_cast<std::size_t>(size), 0));
#endif
    }

    int recv_bytes(char* data, int size) {
#ifdef _WIN32
        return ::recv(handle_, data, size, 0);
#else
        return static_cast<int>(::recv(handle_, data, static_cast<std::size_t>(size), 0));
#endif
    }

private:
    socket_handle handle_ = kInvalidSocket;
};

IpcClient::IpcClient(std::string host, unsigned short port)
    : host_(std::move(host)), port_(port) {}

IpcClient::~IpcClient() {
    stop();
}

bool IpcClient::start(MessageCallback on_message) {
    on_message_ = std::move(on_message);
    running_ = true;
    worker_ = std::thread([this] { worker_loop(); });
    return true;
}

void IpcClient::stop() {
    running_ = false;
    queue_cv_.notify_all();
    if (worker_.joinable()) worker_.join();
}

void IpcClient::enqueue(std::string line) {
    if (line.empty()) return;
    if (line.back() != '\n') line.push_back('\n');
    {
        std::lock_guard<std::mutex> lock(queue_mutex_);
        queue_.push_back(std::move(line));
    }
    queue_cv_.notify_one();
}

void IpcClient::send_json(const Json& payload) {
    enqueue(payload.dump());
}

void IpcClient::send_action_result(const ActionResult& result) {
    Json message = Json::object();
    message["type"] = Json("action_result");
    message["npc_id"] = Json(result.npc_id);
    message["tool"] = Json(result.tool);
    message["request_id"] = Json(result.request_id);
    message["status"] = Json(result.status.empty() ? "failed" : result.status);
    if (!result.reason.empty()) message["reason"] = Json(result.reason);
    send_json(message);
}

void IpcClient::send_ped_scan(const std::string& peds_json) {
    enqueue(peds_json);
}

void IpcClient::send_event(const Json& event_json) {
    send_json(event_json);
}

void IpcClient::send_queued(Socket& socket) {
    std::deque<std::string> pending;
    {
        std::lock_guard<std::mutex> lock(queue_mutex_);
        pending.swap(queue_);
    }
    while (!pending.empty() && running_) {
        const std::string& line = pending.front();
        int sent = 0;
        while (sent < static_cast<int>(line.size())) {
            int result = socket.send_bytes(line.data() + sent, static_cast<int>(line.size()) - sent);
            if (result <= 0) {
                // Re-queue the unsent line and reconnect.
                std::lock_guard<std::mutex> lock(queue_mutex_);
                queue_.push_front(line.substr(static_cast<std::size_t>(sent)));
                return;
            }
            sent += result;
        }
        pending.pop_front();
    }
}

void IpcClient::worker_loop() {
    std::string recv_buffer;
    while (running_) {
        Socket socket;
        if (!socket.connect_to(host_, port_)) {
            std::this_thread::sleep_for(std::chrono::milliseconds(1000));
            continue;
        }
        {
            Json hello = Json::object();
            hello["type"] = Json("hello");
            hello["bridge_version"] = Json("0.2");
            std::lock_guard<std::mutex> lock(queue_mutex_);
            queue_.push_back(hello.dump() + "\n");
        }

        char buffer[8192];
        while (running_ && socket.valid()) {
            send_queued(socket);
            int ready = socket.wait_readable(10);
            if (ready < 0) break;
            if (ready == 0) continue;
            int bytes = socket.recv_bytes(buffer, sizeof(buffer));
            if (bytes <= 0) break;
            recv_buffer.append(buffer, static_cast<std::size_t>(bytes));
            std::size_t pos = 0;
            while ((pos = recv_buffer.find('\n')) != std::string::npos) {
                std::string line = recv_buffer.substr(0, pos);
                recv_buffer.erase(0, pos + 1);
                if (!line.empty() && on_message_) on_message_(line);
            }
        }
        // Requeue any partial line so it is not silently dropped.
        if (!recv_buffer.empty()) {
            // Discard partial JSON after disconnect; a new connection starts clean.
            recv_buffer.clear();
        }
    }
}

}  // namespace rdr2ai
