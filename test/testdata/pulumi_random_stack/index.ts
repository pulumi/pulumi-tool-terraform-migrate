import * as random from "@pulumi/random";

const randomString = new random.RandomString("random", {
  length: 16,
  special: true,
  overrideSpecial: "/@£$",
});

