#pragma once

// Minimal JSON value/parser/serializer used by the bridge.
// Supports null, bool, number, string, array, object.  It is intentionally
// small, dependency-free, and sufficient for the newline-delimited IPC.

#include <cctype>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <map>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

namespace rdr2ai {

class Json {
public:
    enum class Type { Null, Bool, Number, String, Array, Object };

    Json() : type_(Type::Null) {}
    explicit Json(bool value) : type_(Type::Bool), bool_(value) {}
    explicit Json(double value) : type_(Type::Number), number_(value) {}
    explicit Json(int value) : type_(Type::Number), number_(static_cast<double>(value)) {}
    explicit Json(const std::string& value) : type_(Type::String), string_(value) {}
    explicit Json(const char* value) : type_(Type::String), string_(value ? value : "") {}

    static Json object() { Json j; j.type_ = Type::Object; return j; }
    static Json array() { Json j; j.type_ = Type::Array; return j; }

    Type type() const { return type_; }
    bool is_null() const { return type_ == Type::Null; }
    bool is_bool() const { return type_ == Type::Bool; }
    bool is_number() const { return type_ == Type::Number; }
    bool is_string() const { return type_ == Type::String; }
    bool is_array() const { return type_ == Type::Array; }
    bool is_object() const { return type_ == Type::Object; }

    bool as_bool(bool fallback = false) const {
        if (type_ == Type::Bool) return bool_;
        if (type_ == Type::Number) return number_ != 0.0;
        return fallback;
    }
    double as_number(double fallback = 0.0) const {
        return type_ == Type::Number ? number_ : fallback;
    }
    int as_int(int fallback = 0) const {
        return type_ == Type::Number ? static_cast<int>(number_) : fallback;
    }
    float as_float(float fallback = 0.0f) const {
        return type_ == Type::Number ? static_cast<float>(number_) : fallback;
    }
    std::string as_string(const std::string& fallback = "") const {
        return type_ == Type::String ? string_ : fallback;
    }

    bool contains(const std::string& key) const {
        return type_ == Type::Object && object_.find(key) != object_.end();
    }

    const Json& operator[](const std::string& key) const {
        static const Json null_value;
        if (type_ != Type::Object) return null_value;
        auto it = object_.find(key);
        return it == object_.end() ? null_value : it->second;
    }
    Json& operator[](const std::string& key) {
        if (type_ != Type::Object) type_ = Type::Object;
        return object_[key];
    }

    const Json& operator[](std::size_t index) const {
        static const Json null_value;
        if (type_ != Type::Array || index >= array_.size()) return null_value;
        return array_[index];
    }
    Json& operator[](std::size_t index) {
        if (type_ != Type::Array) type_ = Type::Array;
        if (index >= array_.size()) array_.resize(index + 1);
        return array_[index];
    }

    std::size_t size() const {
        if (type_ == Type::Array) return array_.size();
        if (type_ == Type::Object) return object_.size();
        return 0;
    }

    const std::vector<Json>& items() const { return array_; }
    std::vector<Json>& items() { return array_; }
    const std::map<std::string, Json>& fields() const { return object_; }
    std::map<std::string, Json>& fields() { return object_; }

    void push_back(Json value) {
        if (type_ != Type::Array) type_ = Type::Array;
        array_.push_back(std::move(value));
    }

    std::string dump() const {
        std::ostringstream out;
        write(out);
        return out.str();
    }

    static Json parse(const std::string& text) {
        Parser parser(text);
        return parser.parse();
    }

private:
    Type type_;
    bool bool_ = false;
    double number_ = 0.0;
    std::string string_;
    std::vector<Json> array_;
    std::map<std::string, Json> object_;

    void write(std::ostringstream& out) const {
        switch (type_) {
        case Type::Null: out << "null"; break;
        case Type::Bool: out << (bool_ ? "true" : "false"); break;
        case Type::Number:
            if (std::isfinite(number_)) {
                if (number_ == static_cast<long long>(number_)) {
                    out << static_cast<long long>(number_);
                } else {
                    out.precision(10);
                    out << number_;
                }
            } else {
                out << "0";
            }
            break;
        case Type::String: write_string(out, string_); break;
        case Type::Array: {
            out << '[';
            for (std::size_t i = 0; i < array_.size(); ++i) {
                if (i) out << ',';
                array_[i].write(out);
            }
            out << ']';
            break;
        }
        case Type::Object: {
            out << '{';
            bool first = true;
            for (const auto& kv : object_) {
                if (!first) out << ',';
                first = false;
                write_string(out, kv.first);
                out << ':';
                kv.second.write(out);
            }
            out << '}';
            break;
        }
        }
    }

    static void write_string(std::ostringstream& out, const std::string& value) {
        out << '"';
        for (char c : value) {
            switch (c) {
            case '"': out << "\\\""; break;
            case '\\': out << "\\\\"; break;
            case '\b': out << "\\b"; break;
            case '\f': out << "\\f"; break;
            case '\n': out << "\\n"; break;
            case '\r': out << "\\r"; break;
            case '\t': out << "\\t"; break;
            default:
                if (static_cast<unsigned char>(c) < 0x20) {
                    char buffer[8];
                    std::snprintf(buffer, sizeof(buffer), "\\u%04x", static_cast<unsigned char>(c));
                    out << buffer;
                } else {
                    out << c;
                }
            }
        }
        out << '"';
    }

    class Parser {
    public:
        explicit Parser(const std::string& text) : text_(text) {}

        Json parse() {
            skip_ws();
            Json value = parse_value();
            skip_ws();
            if (pos_ != text_.size()) throw std::runtime_error("trailing characters in JSON");
            return value;
        }

    private:
        const std::string& text_;
        std::size_t pos_ = 0;

        void skip_ws() {
            while (pos_ < text_.size()) {
                char c = text_[pos_];
                if (c == ' ' || c == '\t' || c == '\r' || c == '\n') ++pos_;
                else break;
            }
        }

        char peek() const { return pos_ < text_.size() ? text_[pos_] : '\0'; }

        [[noreturn]] void fail(const std::string& message) const {
            throw std::runtime_error("JSON parse error at " + std::to_string(pos_) + ": " + message);
        }

        Json parse_value() {
            switch (peek()) {
            case 'n': return parse_literal("null", Json());
            case 't': return parse_literal("true", Json(true));
            case 'f': return parse_literal("false", Json(false));
            case '"': return Json(parse_string());
            case '[': return parse_array();
            case '{': return parse_object();
            default: return parse_number();
            }
        }

        Json parse_literal(const char* literal, Json value) {
            std::size_t len = std::char_traits<char>::length(literal);
            if (text_.compare(pos_, len, literal) != 0) fail("invalid literal");
            pos_ += len;
            return value;
        }

        std::string parse_string() {
            if (peek() != '"') fail("expected string");
            ++pos_;
            std::string result;
            while (pos_ < text_.size()) {
                char c = text_[pos_++];
                if (c == '"') return result;
                if (c == '\\') {
                    if (pos_ >= text_.size()) fail("unterminated escape");
                    char esc = text_[pos_++];
                    switch (esc) {
                    case '"': result.push_back('"'); break;
                    case '\\': result.push_back('\\'); break;
                    case '/': result.push_back('/'); break;
                    case 'b': result.push_back('\b'); break;
                    case 'f': result.push_back('\f'); break;
                    case 'n': result.push_back('\n'); break;
                    case 'r': result.push_back('\r'); break;
                    case 't': result.push_back('\t'); break;
                    case 'u': {
                        if (pos_ + 4 > text_.size()) fail("invalid unicode escape");
                        unsigned int code = 0;
                        for (int i = 0; i < 4; ++i) {
                            char h = text_[pos_++];
                            code <<= 4;
                            if (h >= '0' && h <= '9') code |= static_cast<unsigned>(h - '0');
                            else if (h >= 'a' && h <= 'f') code |= static_cast<unsigned>(h - 'a' + 10);
                            else if (h >= 'A' && h <= 'F') code |= static_cast<unsigned>(h - 'A' + 10);
                            else fail("invalid hex digit");
                        }
                        // Encode as UTF-8 (BMP only; sufficient for IPC).
                        if (code < 0x80) {
                            result.push_back(static_cast<char>(code));
                        } else if (code < 0x800) {
                            result.push_back(static_cast<char>(0xC0 | (code >> 6)));
                            result.push_back(static_cast<char>(0x80 | (code & 0x3F)));
                        } else {
                            result.push_back(static_cast<char>(0xE0 | (code >> 12)));
                            result.push_back(static_cast<char>(0x80 | ((code >> 6) & 0x3F)));
                            result.push_back(static_cast<char>(0x80 | (code & 0x3F)));
                        }
                        break;
                    }
                    default: fail("unknown escape");
                    }
                } else {
                    result.push_back(c);
                }
            }
            fail("unterminated string");
        }

        Json parse_number() {
            std::size_t start = pos_;
            if (peek() == '-') ++pos_;
            while (pos_ < text_.size() && std::isdigit(static_cast<unsigned char>(text_[pos_]))) ++pos_;
            if (peek() == '.') {
                ++pos_;
                while (pos_ < text_.size() && std::isdigit(static_cast<unsigned char>(text_[pos_]))) ++pos_;
            }
            if (peek() == 'e' || peek() == 'E') {
                ++pos_;
                if (peek() == '+' || peek() == '-') ++pos_;
                while (pos_ < text_.size() && std::isdigit(static_cast<unsigned char>(text_[pos_]))) ++pos_;
            }
            if (pos_ == start) fail("invalid number");
            return Json(std::strtod(text_.substr(start, pos_ - start).c_str(), nullptr));
        }

        Json parse_array() {
            if (peek() != '[') fail("expected array");
            ++pos_;
            Json result = Json::array();
            skip_ws();
            if (peek() == ']') { ++pos_; return result; }
            while (true) {
                skip_ws();
                result.push_back(parse_value());
                skip_ws();
                if (peek() == ',') { ++pos_; continue; }
                if (peek() == ']') { ++pos_; return result; }
                fail("expected ',' or ']'");
            }
        }

        Json parse_object() {
            if (peek() != '{') fail("expected object");
            ++pos_;
            Json result = Json::object();
            skip_ws();
            if (peek() == '}') { ++pos_; return result; }
            while (true) {
                skip_ws();
                if (peek() != '"') fail("expected object key");
                std::string key = parse_string();
                skip_ws();
                if (peek() != ':') fail("expected ':'");
                ++pos_;
                skip_ws();
                result[key] = parse_value();
                skip_ws();
                if (peek() == ',') { ++pos_; continue; }
                if (peek() == '}') { ++pos_; return result; }
                fail("expected ',' or '}'");
            }
        }
    };
};

}  // namespace rdr2ai
