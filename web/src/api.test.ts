import { describe, expect, it } from "vitest";
import { ApiError, errorMessage, parseApiError, unwrapUser } from "./api";

describe("parseApiError", () => {
  it("reads code, message and field from the contract shape", () => {
    const err = parseApiError(
      422,
      JSON.stringify({ error: { code: "invalid", message: "Enter a valid URL.", field: "url" } }),
    );
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(422);
    expect(err.code).toBe("invalid");
    expect(err.message).toBe("Enter a valid URL.");
    expect(err.field).toBe("url");
  });

  it("leaves field undefined when absent", () => {
    const err = parseApiError(403, '{"error":{"code":"email_unverified"}}');
    expect(err.code).toBe("email_unverified");
    expect(err.field).toBeUndefined();
    expect(err.message).toMatch(/permission/);
  });

  it("falls back for empty and non-JSON bodies", () => {
    expect(parseApiError(500, "").code).toBe("http_500");
    expect(parseApiError(502, "<html>Bad gateway</html>").message).toMatch(/went wrong/);
    expect(parseApiError(404, '{"error":"nope"}').message).toBe("Not found.");
  });

  it("errorMessage handles unknown values", () => {
    expect(errorMessage(new ApiError(400, "x", "Bad"))).toBe("Bad");
    expect(errorMessage("oops")).toBe("Something went wrong.");
  });
});

describe("upgrade_required", () => {
  it("keeps the feature key", () => {
    const err = parseApiError(
      403,
      JSON.stringify({ error: { code: "upgrade_required", message: "Custom domains are part of Pro.", field: "hostname", feature: "custom_domains" } }),
    );
    expect(err.code).toBe("upgrade_required");
    expect(err.feature).toBe("custom_domains");
    expect(err.field).toBe("hostname");
    expect(err.message).toBe("Custom domains are part of Pro.");
  });
});

describe("unwrapUser", () => {
  const user = { id: "1", email: "a@b.c", email_verified: false, is_admin: false, created_at: "" };
  it("accepts wrapped and bare users", () => {
    expect(unwrapUser({ user })).toEqual(user);
    expect(unwrapUser(user)).toEqual(user);
  });
});
