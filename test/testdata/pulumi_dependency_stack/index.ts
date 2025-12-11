import * as random from "@pulumi/random";

const randomString = new random.RandomString("random", {
  length: 16,
  special: true,
  overrideSpecial: "/@£$",
});

const randomString2 = new random.RandomString("random2", {
  length: 16,
  special: true,
  overrideSpecial: "/@£$",
});

const randomString3 = new random.RandomString("random3", {
  length: 16,
  special: true,
  overrideSpecial: "/@£$",
});

const randomShuffle = new random.RandomShuffle("random_shuffle", {
  inputs: [randomString.result, randomString2.result, randomString3.result],
  seed: "3",
});
