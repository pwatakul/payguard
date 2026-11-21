import assert from "node:assert/strict";
import test from "node:test";
import {
  applyMailboxPriceUnit, containsMailbox, maskMailboxInstruction, merchantEmailShape,
  merchantMailboxDigest, needsMailboxPriceUnit, parseMailboxBlock, parseScenarioEmail,
} from "../app/merchant-mailbox.ts";

test("masks the exact address without changing the other natural-language clauses", () => {
  assert.deepEqual(maskMailboxInstruction("block email buyer@example.test and product = flight and price > 5000"), {
    mailbox: "buyer@example.test",
    instruction: "block email [EXACT_EMAIL] and product = flight and price > 5000",
  });
  assert.equal(maskMailboxInstruction("Block buyer@example.test or other@example.test"), null);
  assert.equal(maskMailboxInstruction("Block buyer@example.test and [EXACT_EMAIL]"), null);
  assert.equal(maskMailboxInstruction("Block *@example.test"), null);
});

test("adds the chosen price unit without dropping conditions or changing the threshold", () => {
  const english = "Block buyer@example.test and product = flight and price > 5,000";
  const thai = "บล็อกอีเมล buyer@example.test เมื่อเป็นเที่ยวบินและราคามากกว่า 5000";
  assert.equal(needsMailboxPriceUnit(english), true);
  assert.equal(applyMailboxPriceUnit(english, "THB"), english + " THB");
  assert.equal(applyMailboxPriceUnit(thai, "minor units"), thai + " minor units");
  assert.equal(needsMailboxPriceUnit(thai + " บาท"), false);
  assert.equal(needsMailboxPriceUnit(english + " minor units"), false);
  assert.equal(applyMailboxPriceUnit("price < 5000 and amount > 1000", "THB"), null);
  const withIP = "block email [EXACT_EMAIL] and product = flight and price > 3000 and ip from usa";
  assert.equal(applyMailboxPriceUnit(withIP, "THB"), withIP.replace("3000", "3000 THB"));
});

test("shares only ASCII username shape counts for a full mailbox", () => {
  assert.deepEqual(merchantEmailShape("k8p2x9q4m7z6@gmail.com"), { length: 12, letters: 6, digits: 6, transitions: 11 });
  assert.deepEqual(merchantEmailShape("buyer+order24@gmail.com"), { length: 13, letters: 10, digits: 2, transitions: 1 });
  assert.equal(merchantEmailShape("gmail.com"), undefined);
  assert.equal(merchantEmailShape("ลูกค้า@gmail.com"), undefined);
});

test("interprets a standalone exact block in English or Thai", () => {
  const expected = { mailbox: "buyer@example.test", durationHours: 12 };
  for (const command of [
    "Block email buyer@Example.TEST 12 h.",
    '"Block email buyer@example.test 12 h."',
    "“Block email buyer@example.test for 12 hours”",
    "บล็อกอีเมล buyer@example.test เป็นเวลา 12 ชั่วโมง",
  ]) {
    assert.deepEqual(parseMailboxBlock(command), expected);
  }
});

test("interprets minute-based blocks and keeps the original hour contract", () => {
  for (const [command, duration] of [
    ["block buyer@example.test 2min", { durationMinutes: 2 }],
    ['"BLOCK Email Address buyer@EXAMPLE.TEST for 2 MIN."', { durationMinutes: 2 }],
    ["บล็อกอีเมล buyer@example.test เป็นเวลา 2 นาที", { durationMinutes: 2 }],
    ["Block buyer@example.test for 1 minute", { durationMinutes: 1 }],
    ["Block buyer@example.test for 525600 minutes", { durationMinutes: 525600 }],
    ["Block buyer@example.test for 8760 hours", { durationHours: 8760 }],
  ]) {
    assert.deepEqual(parseMailboxBlock(command), { mailbox: "buyer@example.test", ...duration });
  }
});

test("leaves compound, mismatched and invalid commands out of exact blocking", () => {
  for (const command of [
    '"Block email buyer@example.test for 12 hours" for hotels only',
    "“Block email buyer@example.test for 12 hours and review other bookings”",
    "“Block email buyer@example.test for 12 hours\"",
    "Block email buyer@example.test for 8761 hours",
    "Block buyer@example.test for 525601 minutes",
    "Block buyer@example.test for 0 minutes",
    "Block buyer@example.test for 2.5min",
    "Block buyer@example.test for 1h 2min",
    "Block buyer@example.test for 120 seconds",
    "Block buyer@example.test for 2min and review other bookings",
    "Block email *@example.test for 12 hours",
  ]) {
    assert.equal(parseMailboxBlock(command), null);
  }
  assert.equal(containsMailbox("Review flight bookings by buyer@example.test"), true);
  assert.equal(containsMailbox("Block flight bookings from *@example.test"), false);
});

test("uses matching evidence for the same mailbox and separates other mailboxes and merchants", async () => {
  const command = parseMailboxBlock("Block email buyer+events@EXAMPLE.TEST 12 h.");
  const evidence = parseScenarioEmail("  buyer+events@example.test. ");
  assert.deepEqual(evidence, { domain: "example.test", mailbox: "buyer+events@example.test" });
  assert.deepEqual(parseScenarioEmail("EXAMPLE.TEST"), { domain: "example.test" });
  const merchant = "1A56CC97-ED81-4B6C-939C-51EB08978F4A";
  const reference = await merchantMailboxDigest(merchant, command.mailbox);
  assert.match(reference, /^[0-9a-f]{64}$/);
  assert.equal(await merchantMailboxDigest(merchant.toLowerCase(), evidence.mailbox), reference);
  assert.notEqual(await merchantMailboxDigest(merchant, "buyer@example.test"), reference);
  assert.notEqual(await merchantMailboxDigest("7bd5864e-852b-11ee-b9d1-0242ac120002", evidence.mailbox), reference);
});
