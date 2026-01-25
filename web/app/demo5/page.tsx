"use client";

import { Box, TextField } from "@mui/material";
import { useState } from "react";
import { isValid, parseCIDR, parse, IPv4, IPv6 } from "ipaddr.js";

const dn42Range4 = parseCIDR("172.20.0.0/14");
const dn42Range6 = parseCIDR("fd00::/8");
const neoNet4 = parseCIDR("10.127.0.0/16");
const neoNet6 = parseCIDR("fd10:127::/32");

export type TestResult = {
  isDN42IPv4: boolean;
  isDN42IPv6: boolean;
  isDN42IP: boolean;
  isNeoV4: boolean;
  isNeoV6: boolean;
  isNeoIP: boolean;
  isValidIP: boolean;
  addrObj?: IPv4 | IPv6;
  isNeoDomain: boolean;
  isDN42Domain: boolean;
};

function testIP(v: string): TestResult {
  const isValidIP = isValid(v);
  const addrObj = isValidIP ? parse(v) : undefined;
  console.log("[dbg] addrObj:", addrObj);

  let isDN42IPv4 = false;
  let isDN42IPv6 = false;
  let isDN42IP = false;
  let isNeoV4 = false;
  let isNeoV6 = false;
  let isNeoIP = false;
  let isNeoDomain = false;
  let isDN42Domain = false;
  if (addrObj) {
    if (addrObj.kind() === "ipv4") {
      isDN42IPv4 = !!addrObj.match(dn42Range4);
      isNeoV4 = !!addrObj.match(neoNet4);
    } else if (addrObj.kind() === "ipv6") {
      isDN42IPv6 = !!addrObj.match(dn42Range6);
      isNeoV6 = !!addrObj.match(neoNet6);
    }
    isDN42IP = isDN42IPv4 || isDN42IPv6;
    isNeoIP = isNeoV4 || isNeoV6;
  } else {
    isNeoDomain = v.endsWith(".neo") || v.endsWith(".neo.");
    isDN42Domain = v.endsWith(".dn42") || v.endsWith(".dn42.");
  }

  return {
    isDN42IPv4,
    isDN42IPv6,
    isDN42IP,
    isNeoV4,
    isNeoV6,
    isNeoIP,
    isValidIP,
    addrObj,
    isNeoDomain,
    isDN42Domain,
  };
}

export default function Page() {
  const [v, setV] = useState("");

  const {
    isDN42IPv4,
    isDN42IPv6,
    isDN42IP,
    isNeoV4,
    isNeoV6,
    isNeoIP,
    isValidIP,
    isNeoDomain,
    isDN42Domain,
  } = testIP(v);

  return (
    <Box>
      <TextField
        variant="standard"
        value={v}
        onChange={(e) => setV(e.target.value)}
      />
      <Box>IsValidIP: {String(isValidIP)}</Box>
      <Box>IsDN42V4: {String(isDN42IPv4)}</Box>
      <Box>IsDN42V6: {String(isDN42IPv6)}</Box>
      <Box>IsDN42IP: {String(isDN42IP)}</Box>
      <Box>IsNeoV4: {String(isNeoV4)}</Box>
      <Box>IsNeoV6: {String(isNeoV6)}</Box>
      <Box>IsNeo: {String(isNeoIP)}</Box>
      <Box>IsNeoDomain: {String(isNeoDomain)}</Box>
      <Box>IsDN42Domain: {String(isDN42Domain)}</Box>
    </Box>
  );
}
