# Why UniRouter

[简体中文](why-unirouter.zh-CN.md) · [Project overview](../../README.md)

Enterprises need to understand how AI executes, how charges arise, and what
facts support their decisions. UniRouter starts with model execution: access,
routing, usage, billing and inspectable records. X-Insight extends that
foundation by organizing facts from different sources into evidence people can
investigate.

## The experience behind the project

I spent more than twenty years in enterprise sales, working across HP, Dell,
Oracle, SAP, and later open-source and startup businesses. When AI became
practically useful, I felt a real urgency to learn. I wondered how far my
existing experience could take me without understanding this change firsthand.

After leaving a startup and taking my postgraduate entrance examination, I
began exploring robotics and AI in early 2026. AI infrastructure offered a
starting point connected to my experience with infrastructure, databases and
enterprise applications. Paul Graham's idea of
[staying upwind](https://www.paulgraham.com/hs.html) helped: choose work that
opens up promising future options. I could begin with a concrete problem and
learn through building.

In conversations with CIOs, I encountered two approaches. Some companies were
moving quickly under executive sponsorship, while security and data governance
had yet to catch up. Others hesitated because they could not establish what
would happen to their sensitive information.

One concern was especially concrete: employees might send confidential
business material to an external model while simply trying to get their work
done. Intentional disclosure was also a concern, but ordinary use alone raised
questions about what could leave the organization, who could authorize it, and
what evidence would remain afterward.

AI makes knowledge easier to extract, reorganize and reuse. I wanted to explore
how enterprises could benefit from that ability while retaining control over
their information and building on what they learn.

## Why start with a model gateway

A gateway provides a place to apply policy to the model requests that pass
through it. Applications and agents can share an access point, while operators
manage permissions, eligible models, budgets and execution records.

Existing gateways already address important parts of this problem. LiteLLM
provides routing, budget controls, spend tracking and guardrail integrations.
New API provides channel management, quota accounting and a pre-consumption,
settlement and refund lifecycle. Self-hosting, compatibility and recording
usage are shared capabilities; they do not by themselves distinguish UniRouter.

UniRouter focuses on making the basis of execution inspectable: which qualified
configuration allowed a call, which upstream attempts occurred, where usage
came from, and how it became a charge. The
[source-based comparison](gateway-design-comparison.md) explains the mechanisms
behind this focus and the limits of the comparison.

For example, an application may retry after receiving no answer. Understanding
the outcome requires distinguishing upstream attempts, delivery to the client,
reported usage and the final billing state. A successful HTTP response alone
cannot answer all of those questions.

## The choices that follow

**Bind execution to a checked configuration.** In the canonical execution path,
UniRouter checks the selected release, sealed route manifest and applicable
provider qualification before dispatch. It commits the route decision and
provider-attempt record before authorizing the upstream call. This gives a
later investigation a concrete execution basis. It also adds qualification
work and a database dependency to the request path.

**Keep financial facts recoverable and inspectable.** Reservations preserve the
execution binding, billing mode and pricing basis. Settlement and its outgoing
financial event are committed together, with a recovery path for pending
settlements. Provider-reported usage, estimates, calculated amounts and customer
charges retain distinct meanings. This provides a basis for explaining charges
and investigating failures. Its effectiveness still needs verification in each
supported operating environment.

**Make data collection deliberate.** Self-hosting gives an organization control
over its gateway and local records. Requests sent to a remote provider still
leave that environment. Collection, access, retention and export each need
explicit boundaries. The implementation includes optional, scoped content
capture with retention and size limits, and separately authorized access.
Capture controls are different from detecting or blocking sensitive outbound
content. They do not establish comprehensive data-loss prevention, nor cover
requests that bypass the gateway.

**Keep decisions with the system responsible for the action.** UniRouter
controls model calls within its scope. Tool actions and business outcomes
remain the responsibility of their own systems. A model's answer does not
prove that an invoice was issued or a customer accepted a proposal.

**Integrate through explicit interfaces.** Familiar model APIs reduce adoption
work, while qualified profiles make differences between providers explicit.
Observation and investigation should be able to evolve independently of the
gateway's execution path. Extensibility needs clear permissions and data
contracts.

## Where X-Insight fits

UniRouter can operate independently. Its responsibility is model execution,
its ledger and its source records. X-Insight is a separate product for
associating evidence across sources and investigating related activity. It
consumes authorized facts asynchronously, without becoming a synchronous
dependency of model requests or taking over UniRouter's execution decisions.

That division lets users start with the gateway and add investigation when
needed. X-Insight's license, releases and supported integrations are defined
separately. The current source contracts distinguish execution context,
identity and cost basis, with versioned records and explicit collection gaps.
Content access has its own authorization boundary. The
[comparison and implementation references](gateway-design-comparison.md)
describe the inspected scope and distinguish interface capabilities from
delivery readiness.

## The longer-term question

I expect more work to be organized around goals shared by people and agents.
Understanding that work will require connecting model execution to tasks,
actions and outcomes recorded in other systems.

My longer-term interest is whether those observations can help an enterprise
continually refine its understanding of business objects, relationships and
processes: a business ontology informed by actual work. Domain knowledge and
human validation would still be essential. Recorded activity by itself cannot
establish intent, causality or success.

UniRouter's existing implementation provides model access, policy, routing,
usage, billing and request records. Supported behavior and verification remain
specific to the version, provider and environment. The
[source comparison](gateway-design-comparison.md) records the inspected
candidate and its remaining delivery limits. Near-term work focuses on
reproducible first-use paths and easier integration;
cross-source investigation evolves independently in X-Insight. Automatic
business understanding and workflow optimization remain a longer-term research
direction.

## Why open source

Building UniRouter made the distance between an idea and a useful enterprise
product much clearer to me. As agents take on more work, their frameworks and
integration requirements keep changing. Maintaining every adaptation alone
would consume time I could not sustain.

I want recurring needs to become reusable product capabilities, so users can
adopt the product more independently and improvements can benefit more than one
deployment. That is the product-led growth I want to pursue.

Opening the project lets users inspect its mechanisms, test its assumptions
and contribute improvements from real deployments. My background is in sales;
I bring experience with customers, markets and adoption, and want to learn from
people with different engineering and product perspectives. Reports,
documentation, tests and carefully scoped integrations all contribute to that
exchange.

I hope to build something useful through that collaboration, while developing
the broader skills this next stage of my career requires. The
[contribution guide](../../CONTRIBUTING.md) is the starting point for working on
the project together.
