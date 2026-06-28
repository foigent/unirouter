UniRouter Design Manifesto v1.0
Version: 1.0


Status: Draft


Audience: Product / Design / Frontend / Founder




1. Product Vision
UniRouter is not a model proxy.


UniRouter is an Enterprise AI Activity Control Plane.


The product exists to help organizations:


    • Control AI usage
    • Observe AI activity
    • Understand AI behavior
    • Govern AI cost and risk

UniRouter is the operational layer between enterprise applications and AI providers.


It is the system of record for enterprise AI activity.




2. Product Positioning
What UniRouter Is
Enterprise AI Gateway


Enterprise AI Activity Platform


AI Governance Foundation


AI Infrastructure Control Plane




What UniRouter Is Not
Not a chatbot.


Not an AI assistant.




                                                         1
Not a prompt playground.


Not a model marketplace.


Not a developer toy.


Not another OneAPI clone.




3. Core Value Proposition
UniRouter provides three layers of value.


Layer 1 — Control
Control access.


Control routing.


Control spending.


Control reliability.


Control compliance.


Without control, enterprises cannot safely adopt AI.




Layer 2 — Visibility
See all AI activity.


See model usage.


See failures.


See latency.


See spending.


See organizational behavior.


Without visibility, control has no feedback loop.




                                                       2
Layer 3 — Intelligence
Understand patterns.


Identify anomalies.


Recommend optimizations.


Explain costs.


Reveal hidden usage trends.


Without intelligence, visibility remains raw data.




4. Design Philosophy
Principle 1
Show activity, not configuration.


Bad:


    • Number of channels
    • Number of API keys

Good:


    • AI requests today
    • Success rate
    • Top models
    • Usage trend

Users care about activity.


They do not care about database records.




Principle 2
Operational awareness first.


Every screen should answer:


    • Is the system healthy?
    • What changed?
    • What needs attention?




                                                     3
Before answering:


    • How is it configured?




Principle 3
Signal over decoration.


Charts must explain.


Colors must communicate.


Motion must guide.


Visual effects must never exist for decoration alone.




Principle 4
High information density.


Target users are operators.


Not consumers.


Not marketing visitors.


Not casual users.


The interface should maximize understanding per screen.




Principle 5
Progressive complexity.


Beginners should feel safe.


Experts should feel powerful.


Advanced functionality should emerge progressively.


Never overwhelm first-time users.


Never limit experienced users.




                                                        4
5. Design Personality
UniRouter should feel:


Reliable


Operational


Intelligent


Modern


Focused


Transparent



UniRouter should never feel:


Playful


Experimental


Consumer-oriented


Gaming-inspired


Overly futuristic


Cyberpunk




6. Information Architecture
Level 1 Navigation


Overview


Traffic


Billing


Access


System




                               5
Overview


Dashboard



Traffic


Models


Channels


Keys


Logs


Testing



Billing


Wallet


Pricing


Multipliers


Reconciliation


Grants



Access


Users


Audit Logs



System


Settings




7. Dashboard Philosophy
Dashboard is not a configuration center.




                                           6
Dashboard is not a reporting page.


Dashboard is an operational command center.


Primary question:


"What is happening in enterprise AI right now?"



Dashboard priority:


     1. AI Activity
     2. Health
     3. Cost
     4. Configuration



Homepage hierarchy:


Level 1


Total Requests


Success Rate


Average Latency


Cost Today



Level 2


AI Activity Trend



Level 3


Top Models



Level 4


Channel Health



Level 5


Recent Events




                                                  7
Configuration data must not dominate the dashboard.




8. Entity Hierarchy
Future hierarchy:


AI Activity


Model


Request


User


Channel


Configuration



Model is more important than Channel.


Activity is more important than Model.


Configuration is supporting information.




9. Visual Language
Visual hierarchy:


Activity > Health > Cost > Configuration



Spacing scale:


4


8


12


16


24




                                                8
32


48


64



Radius scale:


8


12


16



Shadow scale:


None


Soft


Elevated



Avoid excessive glassmorphism.


Avoid excessive gradients.


Avoid excessive animation.




10. Color Philosophy
Color communicates state.


Not branding.



Green


Healthy



Yellow


Warning




                                 9
Red


Failure



Blue


Neutral information



Purple


Intelligence and insights



Brand colors should never override operational semantics.




11. Theme System
Themes change visual language.


Themes do not change information architecture.



Official Themes


UniRouter


Cloudflare


GitHub


Vercel



Theme switching must never alter workflows.




12. Future Compatibility
Future products:


X-Insight




                                                  10
X-IAM


Trace


Policy Engine


Cost Analytics


must integrate without redesigning navigation.


The architecture must evolve without structural rewrites.




13. Design Decision Test
Before shipping any UI change ask:


Does it improve control?


Does it improve visibility?


Does it improve understanding?


Does it reduce operational effort?


If the answer is no, the feature should be reconsidered.




Final Principle
UniRouter is not a dashboard for managing settings.


UniRouter is a system for understanding enterprise AI activity.




                                                    11
